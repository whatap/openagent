package ec2state

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whatap/gointernal/lang/license"
	"github.com/whatap/golib/lang/pack"
)

func TestForwarderRetriesOnlyRejectedDestination(t *testing.T) {
	now := time.Unix(1700000000, 0)
	f := &Forwarder{now: func() time.Time { return now }}
	settings := fixtureSettings()
	running := fixtureScrape(strings.NewReplacer("48", "16", "terminated", "running").Replace(validStateBody))
	requireForwardCount(t, f, settings, running, 2)
	terminated := fixtureScrape(validStateBody)
	now = now.Add(time.Minute)
	n, err := f.Forward(settings, terminated, -123, func(pcode, _ int64, _ *pack.ParamPack) error {
		if pcode == 202 {
			return errors.New("fixture queue is full")
		}
		return nil
	})
	if n != 1 || err == nil {
		t.Fatalf("partial enqueue accepted=%d err=%v", n, err)
	}
	retry := requireForwardCount(t, f, settings, terminated, 1)
	if retry[0].Pcode != 202 || retry[0].GetLong("stateCode") != 48 {
		t.Fatal("must retry the failed destination's new state, not the accepted one")
	}
	requireForwardCount(t, f, settings, terminated, 0)

	now = now.Add(5 * time.Minute)
	n, err = f.Forward(settings, terminated, -123, func(_ int64, _ int64, _ *pack.ParamPack) error {
		return errors.New("fixture queue is still full")
	})
	if n != 0 || err == nil {
		t.Fatalf("rejected refresh accepted=%d err=%v", n, err)
	}
	requireForwardCount(t, f, settings, terminated, 2)
	requireForwardCount(t, f, settings, terminated, 0)
}

func TestForwarderKeepsDestinationHistoriesIndependent(t *testing.T) {
	f := new(Forwarder)
	settings := fixtureSettings()
	licenses := strings.Split(settings["ec2_state_licenses"], ",")
	settings["ec2_state_licenses"] = licenses[0]
	raw := fixtureScrape(validStateBody)
	requireForwardCount(t, f, settings, raw, 1)
	settings["ec2_state_licenses"] = strings.Join(licenses, ",")
	added := requireForwardCount(t, f, settings, raw, 1)
	if added[0].Pcode != 202 {
		t.Fatal("new destination did not get its first observation")
	}
	licenses[0] = license.Build(101, bytes.Repeat([]byte{9}, 16))
	settings["ec2_state_licenses"] = strings.Join(licenses, ",")
	rotated := requireForwardCount(t, f, settings, raw, 1)
	if rotated[0].Pcode != 101 {
		t.Fatal("rotated credential must get an initial observation")
	}
	settings["ec2_state_licenses"] = " " + licenses[1] + "," + licenses[0] + "," + licenses[0] + " "
	requireForwardCount(t, f, settings, raw, 0)
	requireForwardCount(t, new(Forwarder), settings, raw, 2) // Restart has no delivery history.
	settings["ec2_state_param_id"] = "190002"                // Fixture command change.
	requireForwardCount(t, f, settings, raw, 2)
	for _, want := range []int{2, 0} {
		n, err := f.Forward(settings, raw, -456, func(_ int64, _ int64, _ *pack.ParamPack) error { return nil })
		if n != want || err != nil {
			t.Fatalf("sender identity change: accepted=%d want=%d err=%v", n, want, err)
		}
	}
}

func TestForwarderKeepsSourceHistoriesIndependent(t *testing.T) {
	f := new(Forwarder)
	settings := fixtureSettings()
	a := fixtureScrape(validStateBody)
	b := fixtureScrape(validStateBody)
	b.TargetURL = "http://other-source.invalid/metrics"
	settings["ec2_state_exporter_urls"] = a.TargetURL + "," + b.TargetURL
	requireForwardCount(t, f, settings, a, 2)
	requireForwardCount(t, f, settings, b, 2)
	requireForwardCount(t, f, settings, fixtureScrape(""), 0)
	requireForwardCount(t, f, settings, b, 0) // Source A's empty snapshot cannot clear B.
	requireForwardCount(t, f, settings, a, 2)
	settings["ec2_state_exporter_urls"] = b.TargetURL
	requireForwardCount(t, f, settings, b, 0)
	settings["ec2_state_exporter_urls"] += "," + a.TargetURL
	requireForwardCount(t, f, settings, a, 2)
}

func TestForwarderSendsOnlyNewInstance(t *testing.T) {
	f := new(Forwarder)
	settings := fixtureSettings()
	a := validStateBody
	b := strings.ReplaceAll(a, "i-0123456789abcdef0", "i-1123456789abcdef0")
	requireForwardCount(t, f, settings, fixtureScrape(a), 2)
	for _, p := range requireForwardCount(t, f, settings, fixtureScrape(a+b), 2) {
		if p.GetString("instanceId") != "i-1123456789abcdef0" {
			t.Fatal("unchanged instance was sent instead of the new one")
		}
	}
	requireForwardCount(t, f, settings, fixtureScrape(b), 0)
	requireForwardCount(t, f, settings, fixtureScrape(a+b), 2)
}

func TestForwarderRejectsInvalidResendIntervalWithoutMutatingHistory(t *testing.T) {
	for _, interval := range []string{"0s", "-1m", "five", "999999999999999999h"} {
		t.Run(interval, func(t *testing.T) {
			f := new(Forwarder)
			settings := fixtureSettings()
			raw := fixtureScrape(validStateBody)
			requireForwardCount(t, f, settings, raw, 2)
			settings["ec2_state_resend_interval"] = interval
			n, err := f.Forward(settings, raw, -123, func(_ int64, _ int64, _ *pack.ParamPack) error {
				t.Fatal("invalid interval reached the queue")
				return nil
			})
			if n != 0 || err == nil || !strings.HasPrefix(err.Error(), "EC2_STATE_CONFIG:") {
				t.Fatalf("accepted=%d err=%v", n, err)
			}
			settings["ec2_state_resend_interval"] = "5m"
			requireForwardCount(t, f, settings, raw, 0)
		})
	}
}

func TestForwarderDoesNotRefreshFromMissingOrInvalidObservations(t *testing.T) {
	now := time.Unix(1700000000, 0)
	f := &Forwarder{now: func() time.Time { return now }}
	settings := fixtureSettings()
	requireForwardCount(t, f, settings, fixtureScrape(validStateBody), 2)
	now = now.Add(5 * time.Minute)
	invalid := fixtureScrape(validStateBody + strings.ReplaceAll(validStateBody, "terminated", "running"))
	n, err := f.Forward(settings, invalid, -123, func(_ int64, _ int64, _ *pack.ParamPack) error {
		t.Fatal("invalid snapshot caused a cached replay")
		return nil
	})
	if n != 0 || err == nil {
		t.Fatalf("accepted=%d err=%v", n, err)
	}
	requireForwardCount(t, f, settings, fixtureScrape(validStateBody), 2)
	now = now.Add(5 * time.Minute)
	requireForwardCount(t, f, settings, fixtureScrape("ordinary_metric 7\n"), 0)
	if len(f.sent) != 0 || f.order.Len() != 0 {
		t.Fatal("empty snapshot retained orphaned cache entries")
	}
}

func TestForwarderReloadsIntervalAndRecoversFromClockReversal(t *testing.T) {
	start := time.Unix(1700000000, 0)
	now := start
	f := &Forwarder{now: func() time.Time { return now }}
	settings := fixtureSettings()
	raw := fixtureScrape(validStateBody)
	requireForwardCount(t, f, settings, raw, 2)
	now = start.Add(2 * time.Minute)
	settings["ec2_state_resend_interval"] = "1m"
	requireForwardCount(t, f, settings, raw, 2)
	settings["ec2_state_resend_interval"] = "10m"
	now = start.Add(3 * time.Minute)
	requireForwardCount(t, f, settings, raw, 0)
	now = start.Add(12 * time.Minute)
	requireForwardCount(t, f, settings, raw, 2)
	now = start.Add(-time.Minute)
	requireForwardCount(t, f, settings, raw, 2)
}

func TestForwarderSerializesConcurrentScrapes(t *testing.T) {
	f := new(Forwarder)
	settings := fixtureSettings()
	raw := fixtureScrape(validStateBody)
	var queued atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.Forward(settings, raw, -123, func(_ int64, _ int64, _ *pack.ParamPack) error {
				queued.Add(1)
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := queued.Load(); got != 2 {
		t.Fatalf("concurrent scrapes queued=%d want=2", got)
	}
}
