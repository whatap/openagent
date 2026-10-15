package ec2state

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whatap/golib/lang/pack"

	"open-agent/pkg/model"
)

func TestForwarderAutoSourcesKeepIndependentFiveMinuteCadence(t *testing.T) {
	settings := map[string]string{
		"ec2_state_forwarding_enabled": "true",
		"ec2_state_licenses":           fixtureLicense(101) + "," + fixtureLicense(202),
	}
	start := time.Unix(1700000000, 0)
	now := start
	f := &Forwarder{now: func() time.Time { return now }}
	for _, step := range []struct {
		source string
		after  time.Duration
		want   int
	}{
		{"a", 0, 2},
		{"b", time.Minute, 2},
		{"a", 2 * time.Minute, 0},
		{"b", 5*time.Minute - time.Millisecond, 0},
		{"a", 5*time.Minute - time.Millisecond, 0},
		{"a", 5 * time.Minute, 2},
		{"b", 5 * time.Minute, 0},
		{"b", 6*time.Minute - time.Millisecond, 0},
		{"b", 6 * time.Minute, 2},
		{"a", 6 * time.Minute, 0},
	} {
		now = start.Add(step.after)
		raw := fixtureScrape("# TYPE aws_ec2_instance_state gauge\naws_ec2_instance_state{instance_id=\"i-0123456789abcdef0\",state_name=\"running\",region=\"ap-northeast-2\"} 16\n")
		raw.TargetURL = "https://" + step.source + ".invalid/metrics"
		raw.CollectionTime = now.UnixMilli()
		t.Logf("source=%s elapsed=%s", step.source, step.after)
		for _, p := range requireForwardCount(t, f, settings, raw, step.want) {
			if p.Id != 1002 || p.Time != raw.CollectionTime || p.GetLong("stateCode") != 16 || p.GetString("stateName") != "running" {
				t.Fatal("refresh must preserve the current observation, not a cached timestamp")
			}
		}
	}
}

func TestForwarderAutoSourcesPruneOnlyValidSourceSnapshots(t *testing.T) {
	licenses := []string{fixtureLicense(101), fixtureLicense(202)}
	settings := map[string]string{
		"ec2_state_forwarding_enabled": "true",
		"ec2_state_licenses":           licenses[0] + "," + licenses[1],
	}
	const body = "aws_ec2_instance_state{instance_id=\"i-0123456789abcdef0\",state_name=\"running\"} 16\n"
	a, b := fixtureScrape(body), fixtureScrape(body)
	a.TargetURL, b.TargetURL = "https://a.invalid/metrics", "https://b.invalid/metrics"
	f := new(Forwarder)
	requireForwardCount(t, f, settings, a, 2)
	requireForwardCount(t, f, settings, b, 2)

	invalid := *a
	invalid.RawData = "aws_ec2_instance_state{instance_id=\"i-0123456789abcdef0\",state_name=\"terminated\"} 16\n"
	for _, raw := range []*model.ScrapeRawData{nil, &invalid} {
		n, err := f.Forward(settings, raw, -123, func(_ int64, _ int64, _ *pack.ParamPack) error {
			t.Fatal("missing/invalid scrape synthesized an observation")
			return nil
		})
		if n != 0 || err == nil {
			t.Fatalf("invalid source result=%d err=%v", n, err)
		}
		requireForwardCount(t, f, settings, a, 0)
		requireForwardCount(t, f, settings, b, 0)
	}

	missing := *a
	missing.RawData = "aws_ec2_cpuutilization_average 0\n"
	requireForwardCount(t, f, settings, &missing, 0)
	requireForwardCount(t, f, settings, b, 0)
	requireForwardCount(t, f, settings, a, 2)

	// An explicit restriction removes A globally, but never resets allowed B.
	settings["ec2_state_exporter_urls"] = b.TargetURL
	requireForwardCount(t, f, settings, b, 0)
	requireForwardCount(t, f, settings, a, 0)
	delete(settings, "ec2_state_exporter_urls")
	requireForwardCount(t, f, settings, b, 0)
	requireForwardCount(t, f, settings, a, 2)

	// Destination pruning and disable/reset still apply across automatic sources.
	settings["ec2_state_licenses"] = licenses[0]
	requireForwardCount(t, f, settings, a, 0)
	requireForwardCount(t, f, settings, b, 0)
	settings["ec2_state_licenses"] += "," + licenses[1]
	for _, raw := range []*model.ScrapeRawData{a, b} {
		if p := requireForwardCount(t, f, settings, raw, 1)[0]; p.Pcode != 202 {
			t.Fatal("re-added destination must not resend the unchanged destination")
		}
	}
	settings["ec2_state_forwarding_enabled"] = "false"
	requireForwardCount(t, f, settings, nil, 0)
	settings["ec2_state_forwarding_enabled"] = "true"
	requireForwardCount(t, f, settings, a, 2)
	requireForwardCount(t, f, settings, b, 2)
}

func TestForwarderAutoSourcesDeduplicateConcurrentScrapes(t *testing.T) {
	settings := map[string]string{
		"ec2_state_forwarding_enabled": "true",
		"ec2_state_licenses":           fixtureLicense(101) + "," + fixtureLicense(202),
	}
	const body = "aws_ec2_instance_state{instance_id=\"i-0123456789abcdef0\",state_name=\"running\"} 16\n"
	a, b := fixtureScrape(body), fixtureScrape(body)
	a.TargetURL, b.TargetURL = "https://a.invalid/metrics", "https://b.invalid/metrics"
	f := new(Forwarder)
	var queued atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		for _, raw := range []*model.ScrapeRawData{a, b} {
			wg.Add(1)
			go func(raw *model.ScrapeRawData) {
				defer wg.Done()
				_, err := f.Forward(settings, raw, -123, func(_ int64, _ int64, _ *pack.ParamPack) error {
					queued.Add(1)
					return nil
				})
				if err != nil {
					t.Error(err)
				}
			}(raw)
		}
	}
	wg.Wait()
	if got := queued.Load(); got != 4 {
		t.Fatalf("concurrent automatic sources queued=%d want=4", got)
	}
}
