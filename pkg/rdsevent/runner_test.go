package rdsevent

import (
	"testing"
	"time"
)

func newTestRunner(section func() map[string]interface{}, factory *fakeFactory, ready bool, clock *fakeClock, logs *logCapture) *runner {
	c := newTestCollector(factory, (&recordingSink{}).send, clock, logs)
	r := newRunner(section, "", c, func() bool { return ready })
	r.now = clock.Now
	r.sleep = func(time.Duration) {}
	r.infof, r.errorf = logs.fn("INFO"), logs.fn("ERROR")
	return r
}

func TestRunnerStaysIdleWhenDisabled(t *testing.T) {
	for name, section := range map[string]map[string]interface{}{
		"absent":   nil,
		"disabled": {"enabled": false, "regions": []interface{}{"ap-northeast-2"}},
	} {
		t.Run(name, func(t *testing.T) {
			factory := &fakeFactory{}
			r := newTestRunner(func() map[string]interface{} { return section }, factory, true, &fakeClock{now: t0}, &logCapture{})
			for i := 0; i < 3; i++ {
				if wait := r.step(); wait != idlePoll {
					t.Fatalf("wait %v", wait)
				}
			}
			if factory.calls != 0 {
				t.Fatalf("a disabled collector created %d AWS clients", factory.calls)
			}
		})
	}
}

func TestRunnerRunsRoundsWhenEnabled(t *testing.T) {
	clock := &fakeClock{now: t0}
	api := &fakeRDS{clock: clock}
	factory := &fakeFactory{apis: map[string]EventsAPI{"ap-northeast-2": api}}
	logs := &logCapture{}
	section := map[string]interface{}{
		"enabled":   true,
		"regions":   []interface{}{"ap-northeast-2"},
		"stateFile": t.TempDir() + "/state.json",
	}
	ready := false
	r := newTestRunner(func() map[string]interface{} { return section }, factory, false, clock, logs)
	r.ready = func() bool { return ready }

	if wait := r.step(); wait != sessionPoll || api.callCount() != 0 {
		t.Fatalf("before the session: wait %v, %d calls", wait, api.callCount())
	}
	ready = true
	if wait := r.step(); wait != DefaultInterval || api.callCount() != 1 {
		t.Fatalf("first round: wait %v, %d calls", wait, api.callCount())
	}
	if logs.count("RDS event collection enabled") != 1 || logs.count("waiting for the collector session") != 1 {
		t.Fatalf("log:\n%s", logs.all())
	}
	// The first round is logged even without events; later empty rounds are not.
	r.step()
	if n := logs.count("queued 0 event pack(s) [ap-northeast-2=0]"); n != 1 {
		t.Fatalf("round summary logged %d times:\n%s", n, logs.all())
	}

	section = map[string]interface{}{"enabled": false}
	r.step()
	if logs.count("RDS event collection disabled") != 1 {
		t.Fatalf("disable not logged:\n%s", logs.all())
	}
}

func TestRunnerReportsInvalidConfigOnce(t *testing.T) {
	logs := &logCapture{}
	factory := &fakeFactory{}
	section := map[string]interface{}{"enabled": true, "regions": []interface{}{"seoul"}}
	r := newTestRunner(func() map[string]interface{} { return section }, factory, true, &fakeClock{now: t0}, logs)
	for i := 0; i < 3; i++ {
		if wait := r.step(); wait != idlePoll {
			t.Fatalf("wait %v", wait)
		}
	}
	if logs.count("is invalid") != 1 || factory.calls != 0 {
		t.Fatalf("want one config error and no AWS client:\n%s", logs.all())
	}
}

func TestRunnerSurvivesPanics(t *testing.T) {
	logs := &logCapture{}
	r := newTestRunner(func() map[string]interface{} { panic("config exploded") }, &fakeFactory{}, true, &fakeClock{now: t0}, logs)
	if wait := r.safeStep(); wait != panicRetryDelay {
		t.Fatalf("wait %v", wait)
	}
	if logs.count("recovered from a panic") != 1 {
		t.Fatalf("log:\n%s", logs.all())
	}
}
