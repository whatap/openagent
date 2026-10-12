package ec2state

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/whatap/golib/lang/pack"
)

func TestForwardBoundsPerScrapeFanout(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		observations, projects int
	}{
		{"observation bound", 4097, 1},
		{"fanout bound", 257, 64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := fixtureSettings()
			licenses := make([]string, tc.projects)
			for i := range licenses {
				licenses[i] = fixtureLicense(int64(i + 1))
			}
			settings["ec2_state_licenses"] = strings.Join(licenses, ",")
			var body strings.Builder
			for i := 0; i < tc.observations; i++ {
				fmt.Fprintf(&body, "fixture_ec2_state{instanceId=\"i-%017x\",stateCode=\"48\",stateName=\"terminated\"} 1\n", i)
			}
			calls := 0
			n, err := new(Forwarder).Forward(settings, fixtureScrape(body.String()), -123, func(_ int64, _ int64, _ *pack.ParamPack) error { calls++; return nil })
			if n != 0 || calls != 0 || err == nil {
				t.Fatalf("accepted=%d calls=%d hasError=%v", n, calls, err != nil)
			}
		})
	}
}

func TestForwardReportsOnlyLocalQueueAcceptance(t *testing.T) {
	calls := 0
	n, err := new(Forwarder).Forward(fixtureSettings(), fixtureScrape(validStateBody), -123, func(_ int64, _ int64, _ *pack.ParamPack) error {
		calls++
		if calls == 2 {
			return errors.New("sensitive transport detail must not be logged")
		}
		return nil
	})
	if n != 1 || calls != 2 || err == nil || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("accepted=%d calls=%d err=%v", n, calls, err)
	}
}
