package ec2state

import (
	"strings"
	"testing"
	"time"

	"github.com/whatap/golib/lang/pack"
)

func TestForwarderRefreshUsesEachDestinationsActualAcceptanceTime(t *testing.T) {
	start := time.Unix(1700000000, 0)
	now := start
	f := &Forwarder{now: func() time.Time { return now }}
	settings := fixtureSettings()
	n, err := f.Forward(settings, fixtureScrape(validStateBody), -123, func(_ int64, _ int64, _ *pack.ParamPack) error {
		now = now.Add(30 * time.Second) // Destinations accept at +30s and +60s.
		return nil
	})
	if n != 2 || err != nil {
		t.Fatalf("initial accepted=%d err=%v", n, err)
	}
	for _, step := range []struct {
		after time.Duration
		pcode int64
	}{
		{5 * time.Minute, 0},
		{5*time.Minute + 30*time.Second, 101},
		{6*time.Minute - time.Millisecond, 0},
		{6 * time.Minute, 202},
	} {
		now = start.Add(step.after)
		raw := fixtureScrape(validStateBody)
		raw.CollectionTime = now.UnixMilli()
		want := 0
		if step.pcode != 0 {
			want = 1
		}
		for _, p := range requireForwardCount(t, f, settings, raw, want) {
			if p.Pcode != step.pcode {
				t.Fatalf("refresh pcode=%d want=%d", p.Pcode, step.pcode)
			}
		}
	}
}

func TestForwarderInvalidSnapshotPreservesUnexpiredHistory(t *testing.T) {
	start := time.Unix(1700000000, 0)
	now := start
	f := &Forwarder{now: func() time.Time { return now }}
	settings := fixtureSettings()
	requireForwardCount(t, f, settings, fixtureScrape(validStateBody), 2)
	now = start.Add(time.Minute)
	invalid := fixtureScrape(validStateBody + strings.ReplaceAll(validStateBody, "terminated", "running"))
	n, err := f.Forward(settings, invalid, -123, func(_ int64, _ int64, _ *pack.ParamPack) error {
		t.Fatal("invalid snapshot reached the queue")
		return nil
	})
	if n != 0 || err == nil {
		t.Fatalf("invalid snapshot accepted=%d err=%v", n, err)
	}
	requireForwardCount(t, f, settings, fixtureScrape(validStateBody), 0)
	now = start.Add(5 * time.Minute)
	requireForwardCount(t, f, settings, fixtureScrape(validStateBody), 2)
}
