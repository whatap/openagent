package ec2state

import (
	"fmt"
	"strings"
	"testing"

	"github.com/whatap/golib/lang/pack"
)

func TestForwarderBoundsHistoryAcrossExporters(t *testing.T) {
	// The history budget is separate from the validated per-scrape fanout limit.
	const historyBudget = 16384
	const observationsPerSource = 4096
	settings := fixtureSettings()
	urls := []string{"http://source-a.invalid/metrics", "http://source-b.invalid/metrics", "http://source-c.invalid/metrics"}
	settings["ec2_state_exporter_urls"] = strings.Join(urls, ",")
	var body strings.Builder
	for i := 0; i < observationsPerSource; i++ {
		fmt.Fprintf(&body, "fixture_ec2_state{instanceId=\"i-%017x\",stateCode=\"48\",stateName=\"terminated\"} 1\n", i)
	}
	f := new(Forwarder)
	for i, url := range urls {
		raw := fixtureScrape(body.String())
		raw.TargetURL = url
		want := observationsPerSource * 2
		if i == 2 {
			raw.RawData = validStateBody
			want = 2
		}
		n, err := f.Forward(settings, raw, -123, func(_ int64, _ int64, _ *pack.ParamPack) error { return nil })
		if err != nil || n != want {
			t.Fatalf("source=%d accepted=%d want=%d err=%v", i, n, want, err)
		}
		if len(f.sent) > historyBudget {
			t.Fatalf("history=%d exceeds budget=%d", len(f.sent), historyBudget)
		}
	}
	// The newest source must still suppress duplicates after capacity eviction.
	raw := fixtureScrape(validStateBody)
	raw.TargetURL = urls[2]
	requireForwardCount(t, f, settings, raw, 0)
}
