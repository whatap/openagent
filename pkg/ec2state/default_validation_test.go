package ec2state

import (
	"fmt"
	"strings"
	"testing"

	"github.com/whatap/golib/lang/pack"
)

func TestForwarderCanonicalContractRejectsInvalidSnapshotsAtomically(t *testing.T) {
	const valid = "aws_ec2_instance_state{instance_id=\"i-0123456789abcdef0\",state_name=\"running\",region=\"ap-northeast-2\"} 16\n"
	invalid := map[string]string{
		"missing instance":   "aws_ec2_instance_state{state_name=\"running\"} 16\n",
		"missing name":       "aws_ec2_instance_state{instance_id=\"i-1123456789abcdef0\"} 16\n",
		"pending no name":    "aws_ec2_instance_state{instance_id=\"i-1123456789abcdef0\"} 0\n",
		"unknown name":       "aws_ec2_instance_state{instance_id=\"i-1123456789abcdef0\",state_name=\"unknown\"} 16\n",
		"mismatched name":    "aws_ec2_instance_state{instance_id=\"i-1123456789abcdef0\",state_name=\"terminated\"} 16\n",
		"invalid instance":   "aws_ec2_instance_state{instance_id=\"host-1\",state_name=\"running\"} 16\n",
		"conflicting states": "aws_ec2_instance_state{instance_id=\"i-0123456789abcdef0\",state_name=\"terminated\"} 48\n",
	}
	for _, code := range []string{"1", "17", "16.5", "-1", "81", "NaN", "+Inf", "-Inf"} {
		invalid["invalid code "+code] = fmt.Sprintf("aws_ec2_instance_state{instance_id=\"i-1123456789abcdef0\",state_name=\"running\"} %s\n", code)
	}
	for name, body := range invalid {
		t.Run(name, func(t *testing.T) {
			settings := map[string]string{
				"ec2_state_forwarding_enabled": "true",
				"ec2_state_licenses":           fixtureLicense(101) + "," + fixtureLicense(202),
			}
			f := new(Forwarder)
			calls := 0
			n, err := f.Forward(settings, fixtureScrape("# TYPE aws_ec2_instance_state gauge\n"+valid+body), -123, func(_ int64, _ int64, _ *pack.ParamPack) error {
				calls++
				return nil
			})
			if n != 0 || calls != 0 || err == nil || !strings.HasPrefix(err.Error(), "EC2_STATE_INPUT:") {
				t.Fatalf("invalid snapshot accepted=%d calls=%d err=%v", n, calls, err)
			}
			requireForwardCount(t, f, settings, fixtureScrape(valid), 2)
		})
	}
}

func TestForwarderDefaultsDoNotInferStateFromOtherMetrics(t *testing.T) {
	for _, body := range []string{
		"",
		"aws_ec2_cpuutilization_average{instance_id=\"i-0123456789abcdef0\",state_name=\"terminated\"} 48\n",
		"aws_ec2_status_check_failed_maximum{instance_id=\"i-0123456789abcdef0\"} 1\n",
		"aws_ec2_StatusCheckFailed{instance_id=\"i-0123456789abcdef0\"} 1\n",
		"aws_ec2_instance_state_extra{instance_id=\"i-0123456789abcdef0\",state_name=\"terminated\"} 48\n",
		"legacy_ec2_state{instanceId=\"i-0123456789abcdef0\",stateCode=\"48\",stateName=\"terminated\"} 1\n",
	} {
		settings := map[string]string{
			"ec2_state_forwarding_enabled": "true",
			"ec2_state_licenses":           fixtureLicense(101),
		}
		requireForwardCount(t, new(Forwarder), settings, fixtureScrape(body), 0)
	}
}

func TestForwarderCanonicalContractStillDefaultsOff(t *testing.T) {
	for _, enabled := range []string{"", "false"} {
		settings := map[string]string{
			"ec2_state_forwarding_enabled": enabled,
			"ec2_state_licenses":           fixtureLicense(101),
		}
		raw := fixtureScrape("aws_ec2_instance_state{instance_id=\"i-0123456789abcdef0\",state_name=\"terminated\"} 48\n")
		requireForwardCount(t, new(Forwarder), settings, raw, 0)
	}
}
