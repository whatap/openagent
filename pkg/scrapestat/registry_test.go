package scrapestat

import (
	"errors"
	"testing"
	"time"
)

func findStat(t *testing.T, stats []TargetStat, name string) TargetStat {
	t.Helper()
	for _, s := range stats {
		if s.TargetName == name {
			return s
		}
	}
	t.Fatalf("target %q not found in snapshot %+v", name, stats)
	return TargetStat{}
}

// TestSnapshotAggregatesEndpoints is the cardinality guarantee: many endpoints
// belonging to one configured target must collapse into a single series.
func TestSnapshotAggregatesEndpoints(t *testing.T) {
	Reset()
	t.Cleanup(Reset)

	Record("istio/pod-a", "istio-request", "PodMonitor", "default", true, 120, 4096, ErrNone)
	Record("istio/pod-b", "istio-request", "PodMonitor", "default", true, 310, 2048, ErrNone)
	Record("istio/pod-c", "istio-request", "PodMonitor", "default", false, 2000, 0, ErrTimeout)

	stats := Snapshot()
	if len(stats) != 1 {
		t.Fatalf("expected 1 aggregated target, got %d: %+v", len(stats), stats)
	}

	s := stats[0]
	if s.EndpointsTotal != 3 || s.EndpointsUp != 2 {
		t.Errorf("endpoints = %d/%d, want 2/3", s.EndpointsUp, s.EndpointsTotal)
	}
	if s.MaxDurationMs != 2000 {
		t.Errorf("MaxDurationMs = %d, want 2000", s.MaxDurationMs)
	}
	if s.TotalBytes != 6144 {
		t.Errorf("TotalBytes = %d, want 6144", s.TotalBytes)
	}
	if s.ErrorCode != ErrTimeout {
		t.Errorf("ErrorCode = %d, want %d", s.ErrorCode, ErrTimeout)
	}
}

// TestSnapshotSeparatesByNamespace guards the aggregation key: one targetName
// spanning several namespaces must stay distinguishable.
func TestSnapshotSeparatesByNamespace(t *testing.T) {
	Reset()
	t.Cleanup(Reset)

	Record("t/ns1/a", "kube-state-metrics", "ServiceMonitor", "monitoring", true, 10, 100, ErrNone)
	Record("t/ns2/a", "kube-state-metrics", "ServiceMonitor", "kube-system", false, 20, 0, ErrHTTP5xx)

	stats := Snapshot()
	if len(stats) != 2 {
		t.Fatalf("expected 2 series (one per namespace), got %d: %+v", len(stats), stats)
	}
}

// TestRecordOverwritesPerEndpoint verifies entries hold the latest outcome
// rather than accumulating one row per scrape.
func TestRecordOverwritesPerEndpoint(t *testing.T) {
	Reset()
	t.Cleanup(Reset)

	Record("t/a", "app", "PodMonitor", "default", false, 500, 0, ErrConnRefused)
	Record("t/a", "app", "PodMonitor", "default", true, 40, 900, ErrNone)

	s := findStat(t, Snapshot(), "app")
	if s.EndpointsTotal != 1 {
		t.Fatalf("EndpointsTotal = %d, want 1", s.EndpointsTotal)
	}
	if s.EndpointsUp != 1 || s.ErrorCode != ErrNone {
		t.Errorf("expected recovered endpoint, got up=%d errorCode=%d", s.EndpointsUp, s.ErrorCode)
	}
}

// TestSnapshotEvictsStale covers pods removed by a rollout: their entries must
// disappear instead of pinning a target DOWN forever.
func TestSnapshotEvictsStale(t *testing.T) {
	Reset()
	t.Cleanup(func() {
		nowFn = time.Now
		Reset()
	})

	base := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	nowFn = func() time.Time { return base }
	Record("t/old", "app", "PodMonitor", "default", true, 10, 100, ErrNone)

	nowFn = func() time.Time { return base.Add(staleAfter + time.Minute) }
	Record("t/new", "app", "PodMonitor", "default", true, 20, 200, ErrNone)

	s := findStat(t, Snapshot(), "app")
	if s.EndpointsTotal != 1 {
		t.Fatalf("EndpointsTotal = %d, want 1 (stale endpoint should be evicted)", s.EndpointsTotal)
	}
	if s.TotalBytes != 200 {
		t.Errorf("TotalBytes = %d, want 200", s.TotalBytes)
	}
}

func TestRemove(t *testing.T) {
	Reset()
	t.Cleanup(Reset)

	Record("t/a", "app", "PodMonitor", "default", true, 10, 100, ErrNone)
	Remove("t/a")

	if stats := Snapshot(); len(stats) != 0 {
		t.Fatalf("expected empty snapshot, got %+v", stats)
	}
}

func TestRecordIgnoresEmptyTargetID(t *testing.T) {
	Reset()
	t.Cleanup(Reset)

	Record("", "app", "PodMonitor", "default", true, 10, 100, ErrNone)

	if stats := Snapshot(); len(stats) != 0 {
		t.Fatalf("expected empty snapshot, got %+v", stats)
	}
}

func TestClassifyError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int32
	}{
		{"nil", nil, ErrNone},
		{"context timeout", errors.New(`Get "http://x/metrics": context deadline exceeded`), ErrTimeout},
		{"client timeout", errors.New("Client.Timeout exceeded while awaiting headers"), ErrTimeout},
		{"io timeout", errors.New("dial tcp 10.0.0.1:9090: i/o timeout"), ErrTimeout},
		{"refused", errors.New("dial tcp 10.0.0.1:9090: connect: connection refused"), ErrConnRefused},
		{"dns", errors.New(`dial tcp: lookup svc.local: no such host`), ErrDNS},
		{"tls", errors.New("x509: certificate signed by unknown authority"), ErrTLS},
		{"http 404", errors.New("HTTP error: 404 Not Found"), ErrHTTP4xx},
		{"http 503", errors.New("HTTP error: 503 Service Unavailable"), ErrHTTP5xx},
		{"parse", errors.New("failed to parse metrics text"), ErrParse},
		{"unknown", errors.New("something else entirely"), ErrOther},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyError(tc.err); got != tc.want {
				t.Errorf("ClassifyError(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}
