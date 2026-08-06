// Package scrapestat collects the outcome of each scrape so the counter
// manager can report it as a time series.
//
// The scraper and the counter manager cannot reference each other directly:
// StartCounterManager runs before the ScraperManager is constructed, and the
// ScraperManager is a local variable in open.BootOpenAgent. This package plays
// the same decoupling role as pkg/endpoint does for OpenMxEndpointPack - the
// scraper writes through Record, the counter manager reads through Snapshot.
package scrapestat

import (
	"strings"
	"sync"
	"time"
)

// Error codes reported as the errorCode field. Kept as a small closed set on
// purpose: the raw error text is high cardinality and must never become a tag.
const (
	ErrNone        int32 = 0
	ErrTimeout     int32 = 1
	ErrConnRefused int32 = 2
	ErrDNS         int32 = 3
	ErrTLS         int32 = 4
	ErrHTTP4xx     int32 = 5
	ErrHTTP5xx     int32 = 6
	ErrParse       int32 = 7
	ErrOther       int32 = 99
)

// staleAfter drops endpoints that stopped reporting, so that pods removed by a
// rollout do not linger in the snapshot forever. It must stay comfortably above
// the largest practical scrape interval.
const staleAfter = 10 * time.Minute

// entry is the most recent outcome for a single endpoint (one target ID).
type entry struct {
	targetName string
	targetType string
	namespace  string
	ok         bool
	durationMs int64
	bytes      int64
	errorCode  int32
	updatedAt  time.Time
}

// TargetStat is the per-target aggregate handed to the counter manager.
// Endpoints are collapsed into counts so that a target backed by many pods
// stays a single time series instead of one per pod.
type TargetStat struct {
	TargetName     string
	TargetType     string
	Namespace      string
	EndpointsTotal int32
	EndpointsUp    int32
	MaxDurationMs  int64
	TotalBytes     int64
	ErrorCode      int32 // representative failure; ErrNone when every endpoint is up
}

var (
	mu      sync.Mutex
	entries = map[string]*entry{} // keyed by target ID (one entry per endpoint)
	nowFn   = time.Now            // swapped in tests
)

// Record stores the outcome of one scrape. targetID identifies a single
// endpoint; targetName groups endpoints that came from the same configuration
// entry.
func Record(targetID, targetName, targetType, namespace string,
	ok bool, durationMs int64, bytes int64, errorCode int32) {

	if targetID == "" {
		return
	}

	mu.Lock()
	defer mu.Unlock()

	entries[targetID] = &entry{
		targetName: targetName,
		targetType: targetType,
		namespace:  namespace,
		ok:         ok,
		durationMs: durationMs,
		bytes:      bytes,
		errorCode:  errorCode,
		updatedAt:  nowFn(),
	}
}

// Remove drops an endpoint immediately, for when the scraper already knows a
// target is gone and we should not wait for it to go stale.
func Remove(targetID string) {
	mu.Lock()
	defer mu.Unlock()
	delete(entries, targetID)
}

// Snapshot aggregates the current entries by target and namespace, evicting
// stale endpoints along the way. The returned order is not stable.
func Snapshot() []TargetStat {
	mu.Lock()
	defer mu.Unlock()

	cutoff := nowFn().Add(-staleAfter)
	agg := map[string]*TargetStat{}

	for id, e := range entries {
		if e.updatedAt.Before(cutoff) {
			delete(entries, id)
			continue
		}

		key := e.targetName + "\x00" + e.namespace
		s, ok := agg[key]
		if !ok {
			s = &TargetStat{
				TargetName: e.targetName,
				TargetType: e.targetType,
				Namespace:  e.namespace,
			}
			agg[key] = s
		}

		s.EndpointsTotal++
		s.TotalBytes += e.bytes
		if e.durationMs > s.MaxDurationMs {
			s.MaxDurationMs = e.durationMs
		}
		if e.ok {
			s.EndpointsUp++
		} else if s.ErrorCode == ErrNone {
			// First failure seen wins; enough to tell the dashboard why the
			// target is degraded without carrying per-endpoint detail.
			s.ErrorCode = e.errorCode
		}
	}

	out := make([]TargetStat, 0, len(agg))
	for _, s := range agg {
		out = append(out, *s)
	}
	return out
}

// Reset clears all state. Intended for tests.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	entries = map[string]*entry{}
}

// ClassifyError maps a scrape error onto one of the closed error codes.
func ClassifyError(err error) int32 {
	if err == nil {
		return ErrNone
	}
	msg := err.Error()

	switch {
	case strings.Contains(msg, "context deadline exceeded"),
		strings.Contains(msg, "Client.Timeout exceeded"),
		strings.Contains(msg, "i/o timeout"):
		return ErrTimeout

	case strings.Contains(msg, "connection refused"):
		return ErrConnRefused

	case strings.Contains(msg, "no such host"),
		strings.Contains(msg, "server misbehaving"):
		return ErrDNS

	case strings.Contains(msg, "x509"),
		strings.Contains(msg, "tls:"),
		strings.Contains(msg, "certificate"):
		return ErrTLS

	case strings.Contains(msg, "HTTP error: 4"):
		return ErrHTTP4xx

	case strings.Contains(msg, "HTTP error: 5"):
		return ErrHTTP5xx

	case strings.Contains(msg, "parse"),
		strings.Contains(msg, "decode"),
		strings.Contains(msg, "unmarshal"):
		return ErrParse
	}

	return ErrOther
}
