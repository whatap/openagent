package ec2state

import (
	"strings"
	"testing"
	"time"

	"github.com/whatap/golib/lang/pack"

	"open-agent/pkg/model"
)

func requireForwardCount(t *testing.T, f *Forwarder, settings map[string]string, raw *model.ScrapeRawData, want int) []*pack.ParamPack {
	t.Helper()
	var sent []*pack.ParamPack
	n, err := f.Forward(settings, raw, -123, func(_ int64, _ int64, p *pack.ParamPack) error {
		sent = append(sent, pack.ToPack(pack.ToBytesPack(p)).(*pack.ParamPack))
		return nil
	})
	if err != nil || n != want || len(sent) != want {
		t.Fatalf("locally accepted=%d calls=%d want=%d err=%v", n, len(sent), want, err)
	}
	return sent
}

func TestForwarderResendsAfterObservedDisable(t *testing.T) {
	for _, disabled := range []string{"false", ""} {
		t.Run(disabled, func(t *testing.T) {
			f := new(Forwarder)
			settings := fixtureSettings()
			raw := fixtureScrape(validStateBody)
			requireForwardCount(t, f, settings, raw, 2)
			settings["ec2_state_forwarding_enabled"] = disabled
			requireForwardCount(t, f, settings, nil, 0)
			settings["ec2_state_forwarding_enabled"] = "true"
			requireForwardCount(t, f, settings, raw, 2)
		})
	}
}

func TestForwarderForgetRemovedDestinationsAndAbsentInstances(t *testing.T) {
	f := new(Forwarder)
	settings := fixtureSettings()
	allLicenses := settings["ec2_state_licenses"]
	raw := fixtureScrape(validStateBody)
	requireForwardCount(t, f, settings, raw, 2)
	settings["ec2_state_licenses"] = strings.Split(allLicenses, ",")[0]
	requireForwardCount(t, f, settings, raw, 0)
	settings["ec2_state_licenses"] = allLicenses
	readded := requireForwardCount(t, f, settings, raw, 1)
	if readded[0].Pcode != 202 {
		t.Fatal("re-added destination must not resend the unchanged existing destination")
	}
	requireForwardCount(t, f, settings, fixtureScrape("ordinary_metric 7\n"), 0)
	requireForwardCount(t, f, settings, raw, 2)
}

func TestForwarderResendsAtConfiguredInterval(t *testing.T) {
	start := time.Unix(1700000000, 0)
	now := start
	f := &Forwarder{now: func() time.Time { return now }}
	settings := fixtureSettings()
	settings["ec2_state_resend_interval"] = "5m" // Explicit fixture policy only.
	for _, step := range []struct {
		after time.Duration
		want  int
	}{
		{0, 2},
		{time.Minute, 0},
		{5*time.Minute - time.Millisecond, 0},
		{5 * time.Minute, 2},
		{6 * time.Minute, 0},
		{10 * time.Minute, 2},
	} {
		now = start.Add(step.after)
		raw := fixtureScrape(validStateBody)
		raw.CollectionTime = now.UnixMilli()
		for _, p := range requireForwardCount(t, f, settings, raw, step.want) {
			if p.Time != raw.CollectionTime {
				t.Fatal("refresh must carry the current scrape, not the cached observation time")
			}
		}
	}
}
