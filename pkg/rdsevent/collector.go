package rdsevent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/whatap/golib/lang/pack"

	"open-agent/tools/util/logutil"
)

const (
	logID = "RDS_EVENTS"

	// maxRecords is the DescribeEvents page size (the API allows 20-100).
	maxRecords = 100
	// maxPagesPerWindow stops a marker loop; 1000 pages are 100,000 events.
	maxPagesPerWindow = 1000
	// maxThrottleBackoff caps how long a throttled region is skipped.
	maxThrottleBackoff = 15 * time.Minute
	// errorLogEvery limits repeated error lines per region and error class.
	errorLogEvery = 10 * time.Minute
)

// Sink queues one pack for the WhaTap collector. When it fails the round
// stops sending, and every region with an unsent pack keeps its window.
type Sink func(p *pack.TagCountPack) error

// Collector runs DescribeEvents rounds and keeps one cursor per (role, region).
type Collector struct {
	factory ClientFactory
	sink    Sink
	now     func() time.Time
	infof   func(id, format string, v ...interface{})
	warnf   func(id, format string, v ...interface{})
	errorf  func(id, format string, v ...interface{})

	cursors   map[string]*cursor
	statePath string
	// persist is false for the dry run: no state file is read or written.
	persist bool
	limiter *logLimiter
}

// NewCollector returns a collector that sends through sink and keeps its
// cursors in cfg.StateFile.
func NewCollector(factory ClientFactory, sink Sink) *Collector {
	return &Collector{
		factory: factory,
		sink:    sink,
		now:     time.Now,
		infof:   logutil.Infof,
		warnf:   logutil.Printf,
		errorf:  logutil.Errorf,
		persist: true,
		limiter: newLogLimiter(errorLogEvery),
	}
}

// RegionResult describes one region of a round.
type RegionResult struct {
	Region     string
	Start, End time.Time
	Pages      int
	Fetched    int
	Duplicates int
	Queued     int
	// Skipped is set while a throttled region waits for its backoff.
	Skipped bool
	// Advanced is set when the cursor moved to End.
	Advanced bool
	Err      error
	Class    errorClass
}

// CycleResult summarizes a round for the log and for tests.
type CycleResult struct {
	Regions []RegionResult
	Queued  int
	SendErr error
	Panic   interface{}
}

// RunCycle queries every configured region once, sends the new events in
// time order and advances the cursor of each region whose window was fully
// fetched and queued. It recovers from panics so the agent keeps running.
func (c *Collector) RunCycle(ctx context.Context, cfg Config) (res CycleResult) {
	defer func() {
		if r := recover(); r != nil {
			res.Panic = r
			c.errorf(logID, "recovered from a panic during a round; windows are retried next round: %v", r)
		}
	}()
	c.ensureState(cfg.StateFile)
	now := c.now()

	type queuedEvent struct {
		p      *pack.TagCountPack
		key    string
		region int
	}
	var batch []queuedEvent
	res.Regions = make([]RegionResult, len(cfg.Regions))
	fetched := make([]bool, len(cfg.Regions))
	for i, region := range cfg.Regions {
		rr := &res.Regions[i]
		rr.Region = region
		cur := c.cursor(cfg.RoleARN, region)
		if now.Before(cur.nextTry) {
			rr.Skipped = true
			continue
		}
		start, gapFrom := windowStart(cur.lastEnd, now, cfg)
		rr.Start, rr.End = start, now
		if !gapFrom.IsZero() {
			if ok, suppressed := c.limiter.allow(region+"|gap", now); ok {
				c.warnf(logID, "%s: the last collected window is older than maxCatchUp=%s; events between %s and %s are not collected%s",
					region, cfg.MaxCatchUp, gapFrom.UTC().Format(time.RFC3339), start.UTC().Format(time.RFC3339), suppressedNote(suppressed))
			}
		}
		api, err := c.factory.Client(ctx, cfg, region)
		if err != nil {
			c.regionFailed(cfg, cur, rr, err, now)
			continue
		}
		events, pages, err := fetchWindow(ctx, api, start, now)
		rr.Pages = pages
		if err != nil {
			c.regionFailed(cfg, cur, rr, err, now)
			continue
		}
		rr.Fetched = len(events)
		inBatch := make(map[string]bool)
		for _, ev := range events {
			key := eventKey(ev)
			if cur.seen.has(key) || inBatch[key] {
				rr.Duplicates++
				continue
			}
			p, ok := EventToPack(ev, cfg.AgencyName)
			if !ok {
				continue
			}
			inBatch[key] = true
			batch = append(batch, queuedEvent{p: p, key: key, region: i})
		}
		fetched[i] = true
	}

	// One time-ordered stream over all regions, as the agency sent it.
	sort.SliceStable(batch, func(i, j int) bool { return batch[i].p.Time < batch[j].p.Time })
	blocked := make([]bool, len(cfg.Regions))
	for i, item := range batch {
		if err := c.sink(item.p); err != nil {
			res.SendErr = err
			for _, rest := range batch[i:] {
				blocked[rest.region] = true
			}
			if ok, suppressed := c.limiter.allow("send", now); ok {
				c.errorf(logID, "could not queue event packs (%v); %d of %d queued, the rest are fetched again next round%s",
					err, i, len(batch), suppressedNote(suppressed))
			}
			break
		}
		c.cursor(cfg.RoleARN, cfg.Regions[item.region]).seen.add(item.key, item.p.Time)
		res.Regions[item.region].Queued++
		res.Queued++
	}

	changed := res.Queued > 0
	for i, region := range cfg.Regions {
		if !fetched[i] || blocked[i] {
			continue
		}
		cur := c.cursor(cfg.RoleARN, region)
		cur.lastEnd = now
		cur.seen.pruneBefore(now.Add(-cfg.Overlap).UnixMilli())
		cur.throttled, cur.nextTry = 0, time.Time{}
		if cur.failing {
			cur.failing = false
			c.infof(logID, "%s: DescribeEvents works again", region)
		}
		res.Regions[i].Advanced = true
		changed = true
	}
	if changed && c.persist {
		if err := saveState(c.statePath, c.cursors); err != nil {
			if ok, suppressed := c.limiter.allow("state", now); ok {
				c.errorf(logID, "could not save %s; a restart would fall back to initialLookback: %v%s", c.statePath, err, suppressedNote(suppressed))
			}
		}
	}
	return res
}

// windowStart returns the start of the next window and, when the cursor is
// older than maxCatchUp, the start of the part that is skipped.
func windowStart(lastEnd, now time.Time, cfg Config) (start, skippedFrom time.Time) {
	if lastEnd.IsZero() {
		return now.Add(-cfg.InitialLookback), time.Time{}
	}
	start = lastEnd.Add(-cfg.Overlap)
	if floor := now.Add(-cfg.MaxCatchUp); start.Before(floor) {
		return floor, start
	}
	if start.After(now) {
		// The clock went back; never ask for a window that ends before it starts.
		start = now.Add(-cfg.Overlap)
	}
	return start, time.Time{}
}

// fetchWindow reads every page of DescribeEvents for [start, end].
func fetchWindow(ctx context.Context, api EventsAPI, start, end time.Time) ([]types.Event, int, error) {
	input := &rds.DescribeEventsInput{
		StartTime:  aws.Time(start),
		EndTime:    aws.Time(end),
		MaxRecords: aws.Int32(maxRecords),
	}
	var events []types.Event
	markers := make(map[string]bool)
	for page := 1; ; page++ {
		out, err := api.DescribeEvents(ctx, input)
		if err != nil {
			return nil, page - 1, err
		}
		if out == nil {
			return nil, page, errors.New("DescribeEvents returned an empty response")
		}
		events = append(events, out.Events...)
		marker := aws.ToString(out.Marker)
		if marker == "" {
			return events, page, nil
		}
		if markers[marker] {
			return nil, page, fmt.Errorf("DescribeEvents repeated a marker on page %d", page)
		}
		if page >= maxPagesPerWindow {
			return nil, page, fmt.Errorf("DescribeEvents returned more than %d pages for one window", maxPagesPerWindow)
		}
		markers[marker] = true
		next := *input
		next.Marker = aws.String(marker)
		input = &next
	}
}

func (c *Collector) regionFailed(cfg Config, cur *cursor, rr *RegionResult, err error, now time.Time) {
	class := classify(err)
	rr.Err, rr.Class = err, class
	switch class {
	case classThrottled:
		cur.throttled++
		cur.nextTry = now.Add(throttleBackoff(cfg.Interval, cur.throttled))
	case classCredentials:
		c.factory.InvalidateCredentials(cfg)
	}
	cur.failing = true
	if ok, suppressed := c.limiter.allow(rr.Region+"|"+class.String(), now); ok {
		c.errorf(logID, "%s: DescribeEvents failed [%s]; the window from %s is kept: %s. %s%s",
			rr.Region, class, rr.Start.UTC().Format(time.RFC3339), maskAccounts(err.Error()), class.hint(), suppressedNote(suppressed))
	}
}

func throttleBackoff(interval time.Duration, failures int) time.Duration {
	if failures > 4 {
		failures = 4
	}
	backoff := interval << uint(failures)
	if backoff > maxThrottleBackoff {
		backoff = maxThrottleBackoff
	}
	return backoff
}

func (c *Collector) cursor(roleARN, region string) *cursor {
	key := cursorKey(roleARN, region)
	cur, ok := c.cursors[key]
	if !ok {
		cur = newCursor()
		c.cursors[key] = cur
	}
	return cur
}

// ensureState loads the cursors once per state file path.
func (c *Collector) ensureState(path string) {
	if c.cursors != nil && c.statePath == path {
		return
	}
	if !c.persist {
		c.cursors, c.statePath = make(map[string]*cursor), path
		return
	}
	cursors, err := loadState(path)
	if err != nil {
		c.errorf(logID, "state file unreadable, every region starts with initialLookback: %v", err)
		if moved, mvErr := quarantineState(path, c.now()); mvErr == nil {
			c.warnf(logID, "moved the unreadable state file to %s", moved)
		}
		cursors = make(map[string]*cursor)
	} else if len(cursors) > 0 {
		c.infof(logID, "resuming %d cursor(s) from %s", len(cursors), path)
	}
	c.cursors, c.statePath = cursors, path
}

// logLimiter lets one line per key through every period and counts the rest.
type logLimiter struct {
	every      time.Duration
	last       map[string]time.Time
	suppressed map[string]int
}

func newLogLimiter(every time.Duration) *logLimiter {
	return &logLimiter{every: every, last: make(map[string]time.Time), suppressed: make(map[string]int)}
}

func (l *logLimiter) allow(key string, now time.Time) (bool, int) {
	if last, ok := l.last[key]; ok && now.Sub(last) < l.every && now.Sub(last) >= 0 {
		l.suppressed[key]++
		return false, 0
	}
	n := l.suppressed[key]
	l.last[key] = now
	l.suppressed[key] = 0
	return true, n
}

func suppressedNote(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf(" (%d similar messages suppressed)", n)
}

// describe renders a round for the INFO log.
func (r CycleResult) describe() string {
	parts := make([]string, 0, len(r.Regions))
	for _, rr := range r.Regions {
		switch {
		case rr.Skipped:
			parts = append(parts, rr.Region+"=backoff")
		case rr.Err != nil:
			parts = append(parts, rr.Region+"="+rr.Class.String())
		default:
			parts = append(parts, fmt.Sprintf("%s=%d", rr.Region, rr.Queued))
		}
	}
	return fmt.Sprintf("queued %d event pack(s) [%s]", r.Queued, strings.Join(parts, " "))
}
