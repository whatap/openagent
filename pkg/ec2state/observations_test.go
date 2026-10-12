package ec2state

import (
	"fmt"
	"strings"
	"testing"

	"github.com/whatap/golib/lang/pack"
)

func TestForwardRejectsInvalidSnapshotBeforeAnySend(t *testing.T) {
	for name, body := range map[string]string{
		"missing instance":        "fixture_ec2_state{stateCode=\"48\",stateName=\"terminated\"} 1\n",
		"invalid instance":        strings.ReplaceAll(validStateBody, "i-0123456789abcdef0", "host-1"),
		"missing code":            "fixture_ec2_state{instanceId=\"i-0123456789abcdef0\",stateName=\"terminated\"} 1\n",
		"missing name":            "fixture_ec2_state{instanceId=\"i-0123456789abcdef0\",stateCode=\"48\"} 1\n",
		"code-name mismatch":      strings.ReplaceAll(validStateBody, "terminated", "running"),
		"unknown code":            strings.ReplaceAll(validStateBody, "48", "99"),
		"negative code":           strings.ReplaceAll(validStateBody, "48", "-48"),
		"decimal code":            strings.ReplaceAll(validStateBody, "48", "48.0"),
		"NaN":                     strings.TrimSuffix(validStateBody, "1\n") + "NaN\n",
		"infinity":                strings.TrimSuffix(validStateBody, "1\n") + "+Inf\n",
		"negative presence":       strings.TrimSuffix(validStateBody, "1\n") + "-1\n",
		"not a presence flag":     strings.TrimSuffix(validStateBody, "1\n") + "2\n",
		"counter":                 "# TYPE fixture_ec2_state counter\n" + validStateBody,
		"later conflicting state": validStateBody + strings.NewReplacer("48", "16", "terminated", "running").Replace(validStateBody),
		"later bad state":         validStateBody + "fixture_ec2_state{instanceId=\"i-1123456789abcdef0\",stateCode=\"48\",stateName=\"running\"} 1\n",
		"truncated exposition":    validStateBody + "fixture_ec2_state{instanceId=\"unfinished\n",
		"future timestamp":        strings.TrimSuffix(validStateBody, "\n") + " 1700000000001\n",
		"stale timestamp":         strings.TrimSuffix(validStateBody, "\n") + " 1699999000000\n",
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			n, err := new(Forwarder).Forward(fixtureSettings(), fixtureScrape(body), -123, func(_ int64, _ int64, _ *pack.ParamPack) error {
				calls++
				return nil
			})
			if err == nil || n != 0 || calls != 0 {
				t.Fatalf("invalid snapshot forwarded: calls=%d accepted=%d hasError=%v", calls, n, err != nil)
			}
		})
	}
}

func TestForwardPassesAllExplicitStatesWithoutRetirementDecision(t *testing.T) {
	states := []struct {
		code int
		name string
	}{
		{0, "pending"}, {16, "running"}, {32, "shutting-down"},
		{48, "terminated"}, {64, "stopping"}, {80, "stopped"},
	}
	for _, state := range states {
		t.Run(state.name, func(t *testing.T) {
			body := fmt.Sprintf("fixture_ec2_state{instanceId=\"i-0123456789abcdef0\",stateCode=\"%d\",stateName=\"%s\"} 1\n", state.code, state.name)
			n, err := new(Forwarder).Forward(fixtureSettings(), fixtureScrape(body), -123, func(_ int64, _ int64, p *pack.ParamPack) error {
				if p.GetLong("stateCode") != int64(state.code) || p.GetString("stateName") != state.name {
					t.Fatal("observation state was changed")
				}
				return nil
			})
			if n != 2 || err != nil {
				t.Fatalf("accepted=%d err=%v", n, err)
			}
		})
	}
}

func TestForwardIgnoresInactiveSeriesAndDeduplicatesObservations(t *testing.T) {
	for name, body := range map[string]string{
		"duplicate":                  validStateBody + validStateBody,
		"inactive conflicting state": validStateBody + "fixture_ec2_state{instanceId=\"i-0123456789abcdef0\",stateCode=\"16\",stateName=\"running\"} 0\n",
	} {
		t.Run(name, func(t *testing.T) {
			n, err := new(Forwarder).Forward(fixtureSettings(), fixtureScrape(body), -123, func(_ int64, _ int64, _ *pack.ParamPack) error { return nil })
			if err != nil || n != 2 {
				t.Fatalf("accepted=%d err=%v", n, err)
			}
		})
	}
	for _, body := range []string{"", "unrelated_metric 1\n", strings.TrimSuffix(validStateBody, "1\n") + "0\n"} {
		n, err := new(Forwarder).Forward(fixtureSettings(), fixtureScrape(body), -123, func(_ int64, _ int64, _ *pack.ParamPack) error {
			t.Fatal("absence/inactive series synthesized an observation")
			return nil
		})
		if n != 0 || err != nil {
			t.Fatalf("accepted=%d err=%v", n, err)
		}
	}
}

func TestForwardConfiguresExporterLabelsAndCodeValue(t *testing.T) {
	settings := fixtureSettings()
	settings["ec2_state_instance_id_label"] = "instance_id"
	settings["ec2_state_state_name_label"] = "state"
	settings["ec2_state_state_code_source"] = "value"
	raw := fixtureScrape("# TYPE fixture_ec2_state gauge\nfixture_ec2_state{instance_id=\"i-0123456789abcdef0\",state=\"terminated\"} 48 1699999999000\n")
	n, err := new(Forwarder).Forward(settings, raw, -123, func(_ int64, _ int64, p *pack.ParamPack) error {
		if p.GetLong("stateCode") != 48 || p.GetString("instanceId") != "i-0123456789abcdef0" || p.Time != 1699999999000 {
			t.Fatal("mapped observation or explicit observation time changed")
		}
		return nil
	})
	if n != 2 || err != nil {
		t.Fatalf("accepted=%d err=%v", n, err)
	}
}
