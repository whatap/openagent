package rdsevent

import (
	"context"
	"time"

	"open-agent/tools/util/logutil"
)

const (
	// idlePoll is how often a disabled or misconfigured collector looks at
	// the configuration again (scrape_config.yaml is reloaded on change).
	idlePoll = 30 * time.Second
	// sessionPoll is how often the collector checks for the collector session
	// before its first round.
	sessionPoll = 5 * time.Second
	// panicRetryDelay is the pause after a recovered panic.
	panicRetryDelay = time.Minute
	// minRoundTimeout bounds one round, including SDK retries.
	minRoundTimeout = 5 * time.Minute
)

// runner drives the collector from the live configuration.
type runner struct {
	section   func() map[string]interface{}
	home      string
	collector *Collector
	ready     func() bool
	now       func() time.Time
	sleep     func(time.Duration)
	infof     func(id, format string, v ...interface{})
	errorf    func(id, format string, v ...interface{})

	active        bool
	lastSummary   string
	lastConfigErr string
	waitingLogged bool
	// firstRoundLogged is reset when the collector is disabled.
	firstRoundLogged bool
}

// Start runs the RDS event collector in the background. section returns the
// current features.openAgent.awsRdsEvents map (nil when absent) and is read
// before every round, so enabling, disabling and edits take effect without a
// restart. Nothing reaches AWS while the section is absent or disabled.
func Start(section func() map[string]interface{}, home string) {
	r := newRunner(section, home, NewCollector(NewAWSClientFactory(), secureSink), sessionReady)
	go r.loop()
}

func newRunner(section func() map[string]interface{}, home string, collector *Collector, ready func() bool) *runner {
	return &runner{
		section:   section,
		home:      home,
		collector: collector,
		ready:     ready,
		now:       time.Now,
		sleep:     time.Sleep,
		infof:     logutil.Infof,
		errorf:    logutil.Errorf,
	}
}

func (r *runner) loop() {
	for {
		r.sleep(r.safeStep())
	}
}

// safeStep runs one step and turns a panic into a log line and a pause.
func (r *runner) safeStep() (wait time.Duration) {
	defer func() {
		if p := recover(); p != nil {
			r.errorf(logID, "recovered from a panic; retrying in %s: %v", panicRetryDelay, p)
			wait = panicRetryDelay
		}
	}()
	return r.step()
}

// step reads the configuration, runs at most one round and returns how long to
// wait before the next step.
func (r *runner) step() time.Duration {
	var section map[string]interface{}
	if r.section != nil {
		section = r.section()
	}
	cfg, err := ParseConfig(section, r.home)
	if err != nil {
		if msg := err.Error(); msg != r.lastConfigErr {
			r.lastConfigErr = msg
			r.errorf(logID, "features.openAgent.%s is invalid, RDS event collection is off until it is fixed: %v", SectionName, err)
		}
		r.deactivate()
		return idlePoll
	}
	r.lastConfigErr = ""
	if !cfg.Enabled {
		r.deactivate()
		return idlePoll
	}
	if summary := cfg.summary(); !r.active || summary != r.lastSummary {
		r.infof(logID, "RDS event collection enabled: %s", summary)
		r.lastSummary = summary
	}
	r.active = true
	if !r.ready() {
		if !r.waitingLogged {
			r.infof(logID, "waiting for the collector session before the first round")
			r.waitingLogged = true
		}
		return sessionPoll
	}
	started := r.now()
	timeout := cfg.Interval
	if timeout < minRoundTimeout {
		timeout = minRoundTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	res := r.collector.RunCycle(ctx, cfg)
	// The first round after enabling is always logged so an operator can see
	// the collector work even when there are no events yet.
	if res.Queued > 0 || !r.firstRoundLogged {
		r.infof(logID, "%s", res.describe())
		r.firstRoundLogged = true
	}
	wait := cfg.Interval - r.now().Sub(started)
	if wait < time.Second {
		wait = time.Second
	}
	return wait
}

func (r *runner) deactivate() {
	if r.active {
		r.infof(logID, "RDS event collection disabled")
	}
	r.active = false
	r.lastSummary = ""
	r.waitingLogged = false
	r.firstRoundLogged = false
}
