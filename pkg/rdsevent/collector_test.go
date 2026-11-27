package rdsevent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/smithy-go"
)

func apiError(code string) error {
	return &smithy.GenericAPIError{Code: code, Message: "User: arn:aws:sts::111122223333:assumed-role/R/s is not authorized"}
}

func TestFetchWindowFollowsMarkers(t *testing.T) {
	api := &fakeRDS{}
	for i := 0; i < 250; i++ {
		api.add(rdsEvent(fmt.Sprintf("db-%03d", i), "backup", t0.Add(-time.Duration(250-i)*time.Second)), time.Time{})
	}
	start, end := t0.Add(-time.Hour), t0
	events, pages, err := fetchWindow(context.Background(), api, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 250 || pages != 3 {
		t.Fatalf("got %d events in %d pages, want 250 in 3", len(events), pages)
	}
	if aws.ToString(events[0].SourceIdentifier) != "db-000" || aws.ToString(events[249].SourceIdentifier) != "db-249" {
		t.Fatal("events lost or reordered across pages")
	}
	wantMarkers := []string{"", "100", "200"}
	for i, call := range api.calls {
		if got := aws.ToString(call.Marker); got != wantMarkers[i] {
			t.Fatalf("call %d marker = %q, want %q", i, got, wantMarkers[i])
		}
		if !call.StartTime.Equal(start) || !call.EndTime.Equal(end) || aws.ToInt32(call.MaxRecords) != 100 {
			t.Fatalf("call %d = start %v end %v max %d", i, call.StartTime, call.EndTime, aws.ToInt32(call.MaxRecords))
		}
	}
}

type loopingMarker struct{}

func (loopingMarker) DescribeEvents(context.Context, *rds.DescribeEventsInput, ...func(*rds.Options)) (*rds.DescribeEventsOutput, error) {
	return &rds.DescribeEventsOutput{Marker: aws.String("same")}, nil
}

func TestFetchWindowStopsOnRepeatedMarker(t *testing.T) {
	if _, _, err := fetchWindow(context.Background(), loopingMarker{}, t0.Add(-time.Hour), t0); err == nil || !strings.Contains(err.Error(), "repeated a marker") {
		t.Fatalf("err = %v", err)
	}
}

func TestFirstRoundLooksBackOnlyInitialLookback(t *testing.T) {
	clock := &fakeClock{now: t0}
	api := &fakeRDS{clock: clock}
	api.add(rdsEvent("db-1", "old restart", t0.Add(-20*time.Minute), "availability"), time.Time{})
	api.add(rdsEvent("db-1", "recent restart", t0.Add(-5*time.Minute), "availability"), time.Time{})
	sink := &recordingSink{}
	c := newTestCollector(&fakeFactory{apis: map[string]EventsAPI{"ap-northeast-2": api}}, sink.send, clock, &logCapture{})
	cfg := testConfig(t, "ap-northeast-2")

	res := c.RunCycle(context.Background(), cfg)
	if res.Queued != 1 || !reflect.DeepEqual(sink.messages(), []string{"recent restart"}) {
		t.Fatalf("queued %d %v; want only the event inside initialLookback", res.Queued, sink.messages())
	}
	if got := api.lastCall(); !got.StartTime.Equal(t0.Add(-10*time.Minute)) || !got.EndTime.Equal(t0) {
		t.Fatalf("window = %v..%v", got.StartTime, got.EndTime)
	}
}

func TestOverlapCatchesLateEventsWithoutDuplicates(t *testing.T) {
	clock := &fakeClock{now: t0}
	api := &fakeRDS{clock: clock}
	api.add(rdsEvent("db-1", "A", t0.Add(-2*time.Minute)), time.Time{})
	api.add(rdsEvent("db-1", "B", t0.Add(-time.Minute)), time.Time{})
	// Dated before the first round's end but listed only afterwards.
	api.add(rdsEvent("db-2", "late", t0.Add(-30*time.Second)), t0.Add(20*time.Second))
	api.add(rdsEvent("db-1", "C", t0.Add(30*time.Second)), time.Time{})
	sink := &recordingSink{}
	c := newTestCollector(&fakeFactory{apis: map[string]EventsAPI{"ap-northeast-2": api}}, sink.send, clock, &logCapture{})
	cfg := testConfig(t, "ap-northeast-2")

	c.RunCycle(context.Background(), cfg)
	if got := sink.messages(); !reflect.DeepEqual(got, []string{"A", "B"}) {
		t.Fatalf("round 1 sent %v", got)
	}
	sink.reset()
	clock.Set(t0.Add(time.Minute))
	res := c.RunCycle(context.Background(), cfg)
	if got := sink.messages(); !reflect.DeepEqual(got, []string{"late", "C"}) {
		t.Fatalf("round 2 sent %v, want the late event and C once, in time order", got)
	}
	if res.Regions[0].Duplicates != 2 {
		t.Fatalf("duplicates = %d, want A and B", res.Regions[0].Duplicates)
	}
	if got := api.lastCall(); !got.StartTime.Equal(t0.Add(-5 * time.Minute)) {
		t.Fatalf("round 2 start = %v, want previous end minus overlap", got.StartTime)
	}
}

func TestStateFileResumesAfterRestart(t *testing.T) {
	clock := &fakeClock{now: t0}
	api := &fakeRDS{clock: clock}
	api.add(rdsEvent("db-1", "A", t0.Add(-2*time.Minute)), time.Time{})
	api.add(rdsEvent("db-1", "B", t0.Add(-time.Minute)), time.Time{})
	factory := &fakeFactory{apis: map[string]EventsAPI{"ap-northeast-2": api}}
	cfg := testConfig(t, "ap-northeast-2")

	first := &recordingSink{}
	newTestCollector(factory, first.send, clock, &logCapture{}).RunCycle(context.Background(), cfg)
	if len(first.packs) != 2 {
		t.Fatalf("first process sent %d", len(first.packs))
	}
	raw, err := os.ReadFile(cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	var state persistedState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	saved, ok := state.Cursors["default|ap-northeast-2"]
	if !ok || saved.LastEndMs != t0.UnixMilli() || len(saved.Seen) != 2 {
		t.Fatalf("state = %s", raw)
	}

	// The agent restarts ten minutes later: no gap, no duplicates.
	api.add(rdsEvent("db-1", "while stopped", t0.Add(4*time.Minute)), time.Time{})
	clock.Set(t0.Add(10 * time.Minute))
	second := &recordingSink{}
	res := newTestCollector(factory, second.send, clock, &logCapture{}).RunCycle(context.Background(), cfg)
	if got := api.lastCall(); !got.StartTime.Equal(t0.Add(-5 * time.Minute)) {
		t.Fatalf("restart window starts at %v, want saved end minus overlap", got.StartTime)
	}
	if got := second.messages(); !reflect.DeepEqual(got, []string{"while stopped"}) {
		t.Fatalf("after restart sent %v", got)
	}
	if res.Regions[0].Duplicates != 2 {
		t.Fatalf("restored de-duplication missed: %d duplicates", res.Regions[0].Duplicates)
	}
}

func TestCatchUpIsCappedByMaxCatchUp(t *testing.T) {
	clock := &fakeClock{now: t0}
	api := &fakeRDS{clock: clock}
	cfg := testConfig(t, "ap-northeast-2")
	old := map[string]*cursor{"default|ap-northeast-2": {lastEnd: t0.Add(-72 * time.Hour), seen: newSeenSet(0)}}
	if err := saveState(cfg.StateFile, old); err != nil {
		t.Fatal(err)
	}
	logs := &logCapture{}
	c := newTestCollector(&fakeFactory{apis: map[string]EventsAPI{"ap-northeast-2": api}}, (&recordingSink{}).send, clock, logs)
	c.RunCycle(context.Background(), cfg)
	if got := api.lastCall(); !got.StartTime.Equal(t0.Add(-24 * time.Hour)) {
		t.Fatalf("start = %v, want now - maxCatchUp", got.StartTime)
	}
	if logs.count("older than maxCatchUp") != 1 {
		t.Fatalf("skipped range not logged:\n%s", logs.all())
	}
}

func TestRegionsHaveOwnCursorsAndOneTimeOrder(t *testing.T) {
	clock := &fakeClock{now: t0}
	seoul, virginia := &fakeRDS{clock: clock}, &fakeRDS{clock: clock}
	seoul.add(rdsEvent("seoul-db", "s1", t0.Add(-4*time.Minute)), time.Time{})
	seoul.add(rdsEvent("seoul-db", "s2", t0.Add(-2*time.Minute)), time.Time{})
	virginia.add(rdsEvent("us-db", "v1", t0.Add(-3*time.Minute)), time.Time{})
	virginia.add(rdsEvent("us-db", "v2", t0.Add(-time.Minute)), time.Time{})
	sink := &recordingSink{}
	cfg := testConfig(t, "ap-northeast-2", "us-east-1")
	c := newTestCollector(&fakeFactory{apis: map[string]EventsAPI{"ap-northeast-2": seoul, "us-east-1": virginia}}, sink.send, clock, &logCapture{})

	c.RunCycle(context.Background(), cfg)
	if got := sink.messages(); !reflect.DeepEqual(got, []string{"s1", "v1", "s2", "v2"}) {
		t.Fatalf("sent %v, want one stream in time order", got)
	}
	loaded, err := loadState(cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"default|ap-northeast-2", "default|us-east-1"} {
		if cur, ok := loaded[key]; !ok || !cur.lastEnd.Equal(t0) {
			t.Fatalf("cursor %s = %+v", key, cur)
		}
	}
}

func TestErrorKeepsWindowUntilTheRegionRecovers(t *testing.T) {
	clock := &fakeClock{now: t0}
	seoul, virginia := &fakeRDS{clock: clock}, &fakeRDS{clock: clock}
	factory := &fakeFactory{apis: map[string]EventsAPI{"ap-northeast-2": seoul, "us-east-1": virginia}}
	sink := &recordingSink{}
	logs := &logCapture{}
	cfg := testConfig(t, "ap-northeast-2", "us-east-1")
	c := newTestCollector(factory, sink.send, clock, logs)
	c.RunCycle(context.Background(), cfg) // both cursors at t0

	// Seoul is denied for two rounds; Virginia keeps going.
	seoul.errs = []error{fmt.Errorf("operation error RDS: DescribeEvents: %w", apiError("AccessDenied")), apiError("AccessDenied")}
	seoul.add(rdsEvent("seoul-db", "failover during outage", t0.Add(90*time.Second), "failover"), time.Time{})
	virginia.add(rdsEvent("us-db", "v", t0.Add(90*time.Second)), time.Time{})
	clock.Set(t0.Add(2 * time.Minute))
	res := c.RunCycle(context.Background(), cfg)
	if res.Regions[0].Class != classAccessDenied || res.Regions[0].Advanced || !res.Regions[1].Advanced {
		t.Fatalf("round 2 = %+v", res.Regions)
	}
	if got := sink.messages(); !reflect.DeepEqual(got, []string{"v"}) {
		t.Fatalf("round 2 sent %v", got)
	}
	clock.Set(t0.Add(3 * time.Minute))
	c.RunCycle(context.Background(), cfg)

	sink.reset()
	clock.Set(t0.Add(4 * time.Minute))
	res = c.RunCycle(context.Background(), cfg)
	if got := seoul.lastCall(); !got.StartTime.Equal(t0.Add(-5 * time.Minute)) {
		t.Fatalf("Seoul resumed from %v, want its last good end (t0) minus overlap", got.StartTime)
	}
	if got := sink.messages(); !reflect.DeepEqual(got, []string{"failover during outage"}) {
		t.Fatalf("after recovery sent %v", got)
	}
	if logs.count("111122223333") != 0 {
		t.Fatalf("account number reached the log:\n%s", logs.all())
	}
	if logs.count("[access-denied]") != 1 || logs.count("works again") != 1 {
		t.Fatalf("want one rate-limited error line and one recovery line:\n%s", logs.all())
	}
}

func TestExpiredCredentialsAreInvalidated(t *testing.T) {
	clock := &fakeClock{now: t0}
	api := &fakeRDS{clock: clock, errs: []error{apiError("ExpiredToken")}}
	factory := &fakeFactory{apis: map[string]EventsAPI{"ap-northeast-2": api}}
	c := newTestCollector(factory, (&recordingSink{}).send, clock, &logCapture{})
	res := c.RunCycle(context.Background(), testConfig(t, "ap-northeast-2"))
	if res.Regions[0].Class != classCredentials || res.Regions[0].Advanced || factory.invalidated != 1 {
		t.Fatalf("result %+v, invalidated %d", res.Regions[0], factory.invalidated)
	}
	// The next round asks again right away (no backoff for credentials).
	clock.Set(t0.Add(time.Minute))
	if res := c.RunCycle(context.Background(), testConfig(t, "ap-northeast-2")); !res.Regions[0].Advanced {
		t.Fatalf("second round %+v", res.Regions[0])
	}
}

func TestThrottledRegionBacksOff(t *testing.T) {
	clock := &fakeClock{now: t0}
	api := &fakeRDS{clock: clock, errs: []error{apiError("Throttling"), apiError("ThrottlingException")}}
	cfg := testConfig(t, "ap-northeast-2")
	c := newTestCollector(&fakeFactory{apis: map[string]EventsAPI{"ap-northeast-2": api}}, (&recordingSink{}).send, clock, &logCapture{})

	if res := c.RunCycle(context.Background(), cfg); res.Regions[0].Class != classThrottled {
		t.Fatalf("round 1 = %+v", res.Regions[0])
	}
	clock.Set(t0.Add(time.Minute)) // inside the 2x interval backoff
	if res := c.RunCycle(context.Background(), cfg); !res.Regions[0].Skipped || api.callCount() != 1 {
		t.Fatalf("round 2 = %+v after %d calls; want skipped", res.Regions[0], api.callCount())
	}
	clock.Set(t0.Add(2 * time.Minute))
	if res := c.RunCycle(context.Background(), cfg); res.Regions[0].Class != classThrottled {
		t.Fatalf("round 3 = %+v", res.Regions[0])
	}
	if next := c.cursors["default|ap-northeast-2"].nextTry; !next.Equal(t0.Add(6 * time.Minute)) {
		t.Fatalf("second backoff ends at %v, want +4m", next)
	}
	clock.Set(t0.Add(6 * time.Minute))
	if res := c.RunCycle(context.Background(), cfg); !res.Regions[0].Advanced {
		t.Fatalf("round after backoff = %+v", res.Regions[0])
	}
	if got := throttleBackoff(time.Minute, 10); got != maxThrottleBackoff {
		t.Fatalf("backoff cap = %v", got)
	}
}

func TestSendFailureKeepsWindowAndSkipsQueuedEvents(t *testing.T) {
	clock := &fakeClock{now: t0}
	api := &fakeRDS{clock: clock}
	api.add(rdsEvent("db-1", "A", t0.Add(-3*time.Minute)), time.Time{})
	api.add(rdsEvent("db-1", "B", t0.Add(-2*time.Minute)), time.Time{})
	api.add(rdsEvent("db-1", "C", t0.Add(-time.Minute)), time.Time{})
	sink := &recordingSink{failAt: 2, err: errQueueFull}
	cfg := testConfig(t, "ap-northeast-2")
	c := newTestCollector(&fakeFactory{apis: map[string]EventsAPI{"ap-northeast-2": api}}, sink.send, clock, &logCapture{})

	res := c.RunCycle(context.Background(), cfg)
	if res.SendErr == nil || res.Regions[0].Advanced || res.Queued != 1 {
		t.Fatalf("round 1 = %+v", res)
	}
	clock.Set(t0.Add(time.Minute))
	res = c.RunCycle(context.Background(), cfg)
	if got := sink.messages(); !reflect.DeepEqual(got, []string{"A", "B", "C"}) {
		t.Fatalf("sent %v, want each event exactly once", got)
	}
	if got := api.lastCall(); !got.StartTime.Equal(t0.Add(-9*time.Minute)) || !res.Regions[0].Advanced {
		t.Fatalf("retry window starts at %v (advanced %v), want initialLookback from the new now: the cursor did not move", got.StartTime, res.Regions[0].Advanced)
	}
}

func TestPanicDuringRoundIsRecovered(t *testing.T) {
	clock := &fakeClock{now: t0}
	api := &fakeRDS{clock: clock, panic: "boom"}
	logs := &logCapture{}
	cfg := testConfig(t, "ap-northeast-2")
	c := newTestCollector(&fakeFactory{apis: map[string]EventsAPI{"ap-northeast-2": api}}, (&recordingSink{}).send, clock, logs)
	res := c.RunCycle(context.Background(), cfg)
	if res.Panic == nil || logs.count("recovered from a panic") != 1 {
		t.Fatalf("panic not recovered: %+v\n%s", res, logs.all())
	}
	if _, err := os.Stat(cfg.StateFile); !os.IsNotExist(err) {
		t.Fatalf("a panicking round must not advance the state (stat err %v)", err)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		err  error
		want errorClass
	}{
		{apiError("AccessDenied"), classAccessDenied},
		{fmt.Errorf("failed to refresh cached credentials, operation error STS: AssumeRole: %w", apiError("AccessDenied")), classAccessDenied},
		{apiError("ExpiredToken"), classCredentials},
		{apiError("InvalidClientTokenId"), classCredentials},
		{errors.New("failed to refresh cached credentials, no EC2 IMDS role found"), classCredentials},
		{apiError("Throttling"), classThrottled},
		{fmt.Errorf("exceeded maximum number of attempts, 5: %w", apiError("RequestLimitExceeded")), classThrottled},
		{context.DeadlineExceeded, classTimeout},
		{errors.New("dial tcp: no route to host"), classOther},
	}
	for _, tc := range cases {
		if got := classify(tc.err); got != tc.want {
			t.Errorf("classify(%v) = %s, want %s", tc.err, got, tc.want)
		}
	}
}

func TestWindowStart(t *testing.T) {
	cfg := DefaultConfig("")
	cases := []struct {
		name      string
		lastEnd   time.Time
		wantStart time.Time
		wantGap   bool
	}{
		{"first round", time.Time{}, t0.Add(-10 * time.Minute), false},
		{"normal", t0.Add(-time.Minute), t0.Add(-6 * time.Minute), false},
		{"long stop", t0.Add(-48 * time.Hour), t0.Add(-24 * time.Hour), true},
		{"clock went back", t0.Add(time.Hour), t0.Add(-5 * time.Minute), false},
	}
	for _, tc := range cases {
		start, gap := windowStart(tc.lastEnd, t0, cfg)
		if !start.Equal(tc.wantStart) || gap.IsZero() == tc.wantGap {
			t.Errorf("%s: start %v gap %v", tc.name, start, gap)
		}
	}
}

func TestLogLimiter(t *testing.T) {
	l := newLogLimiter(10 * time.Minute)
	if ok, _ := l.allow("k", t0); !ok {
		t.Fatal("first line suppressed")
	}
	for i := 1; i <= 3; i++ {
		if ok, _ := l.allow("k", t0.Add(time.Duration(i)*time.Minute)); ok {
			t.Fatal("repeat within the period let through")
		}
	}
	if ok, n := l.allow("k", t0.Add(11*time.Minute)); !ok || n != 3 {
		t.Fatalf("after the period ok=%v suppressed=%d", ok, n)
	}
}
