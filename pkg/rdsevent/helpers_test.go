package rdsevent

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/whatap/golib/lang/pack"
)

// t0 is the reference "now" of the collector tests.
var t0 = time.Date(2026, 11, 25, 3, 0, 0, 0, time.UTC)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// fakeEvent is an RDS event that DescribeEvents lists only from visibleAt on,
// to model events that show up a little after their date.
type fakeEvent struct {
	ev        types.Event
	visibleAt time.Time
}

// fakeRDS filters events by [StartTime, EndTime] and pages them with numeric
// markers, like DescribeEvents.
type fakeRDS struct {
	mu       sync.Mutex
	clock    *fakeClock
	events   []fakeEvent
	pageSize int
	// errs are returned, one per call, before serving events (nil entries
	// serve normally).
	errs  []error
	calls []rds.DescribeEventsInput
	panic interface{}
}

func (f *fakeRDS) add(ev types.Event, visibleAt time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, fakeEvent{ev: ev, visibleAt: visibleAt})
}

func (f *fakeRDS) DescribeEvents(_ context.Context, in *rds.DescribeEventsInput, _ ...func(*rds.Options)) (*rds.DescribeEventsOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, *in)
	if f.panic != nil {
		panic(f.panic)
	}
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return nil, err
		}
	}
	now := t0
	if f.clock != nil {
		now = f.clock.Now()
	}
	var matched []types.Event
	for _, fe := range f.events {
		if fe.visibleAt.After(now) {
			continue
		}
		if fe.ev.Date.Before(*in.StartTime) || fe.ev.Date.After(*in.EndTime) {
			continue
		}
		matched = append(matched, fe.ev)
	}
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].Date.Before(*matched[j].Date) })
	offset := 0
	if in.Marker != nil {
		var err error
		if offset, err = strconv.Atoi(*in.Marker); err != nil {
			return nil, fmt.Errorf("bad marker %q", *in.Marker)
		}
	}
	size := int(aws.ToInt32(in.MaxRecords))
	if f.pageSize > 0 {
		size = f.pageSize
	}
	end := offset + size
	if end > len(matched) {
		end = len(matched)
	}
	out := &rds.DescribeEventsOutput{Events: append([]types.Event(nil), matched[offset:end]...)}
	if end < len(matched) {
		out.Marker = aws.String(strconv.Itoa(end))
	}
	return out, nil
}

func (f *fakeRDS) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeRDS) lastCall() rds.DescribeEventsInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

type fakeFactory struct {
	mu          sync.Mutex
	apis        map[string]EventsAPI
	clientErr   error
	calls       int
	invalidated int
}

func (f *fakeFactory) Client(_ context.Context, _ Config, region string) (EventsAPI, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.clientErr != nil {
		return nil, f.clientErr
	}
	api, ok := f.apis[region]
	if !ok {
		return nil, fmt.Errorf("no fake RDS for %s", region)
	}
	return api, nil
}

func (f *fakeFactory) InvalidateCredentials(Config) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invalidated++
}

type logCapture struct {
	mu    sync.Mutex
	lines []string
}

func (l *logCapture) fn(level string) func(id, format string, v ...interface{}) {
	return func(id, format string, v ...interface{}) {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.lines = append(l.lines, level+" "+fmt.Sprintf(format, v...))
	}
}

func (l *logCapture) count(substr string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, line := range l.lines {
		if strings.Contains(line, substr) {
			n++
		}
	}
	return n
}

func (l *logCapture) all() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

// recordingSink collects the packs; failAt makes the n-th call (1-based) fail.
type recordingSink struct {
	mu     sync.Mutex
	packs  []*pack.TagCountPack
	calls  int
	failAt int
	err    error
}

func (s *recordingSink) send(p *pack.TagCountPack) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.failAt > 0 && s.calls == s.failAt {
		return s.err
	}
	s.packs = append(s.packs, p)
	return nil
}

func (s *recordingSink) messages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.packs))
	for i, p := range s.packs {
		out[i] = p.Data.GetString(fieldMessage)
	}
	return out
}

func (s *recordingSink) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.packs = nil
}

func rdsEvent(id, message string, date time.Time, categories ...string) types.Event {
	return types.Event{
		Date:             aws.Time(date),
		Message:          aws.String(message),
		SourceArn:        aws.String("arn:aws:rds:ap-northeast-2:111122223333:db:" + id),
		SourceIdentifier: aws.String(id),
		SourceType:       types.SourceTypeDbInstance,
		EventCategories:  categories,
	}
}

func testConfig(t *testing.T, regions ...string) Config {
	t.Helper()
	cfg := DefaultConfig(t.TempDir())
	cfg.Enabled = true
	cfg.Regions = regions
	if err := cfg.Validate(); err != nil {
		t.Fatalf("test config invalid: %v", err)
	}
	return cfg
}

func newTestCollector(factory ClientFactory, sink Sink, clock *fakeClock, logs *logCapture) *Collector {
	c := NewCollector(factory, sink)
	c.now = clock.Now
	c.infof, c.warnf, c.errorf = logs.fn("INFO"), logs.fn("WARN"), logs.fn("ERROR")
	return c
}
