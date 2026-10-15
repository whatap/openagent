package ec2state

import (
	"strings"
	"testing"

	"github.com/whatap/golib/lang/pack"
)

func TestForwarderUsesRegisteredRawSourceWithoutURLSetting(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		omit  bool
	}{
		{"omitted", "", true},
		{"empty", "", false},
		{"whitespace", " \t\n ", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := map[string]string{
				"ec2_state_forwarding_enabled": "true",
				"ec2_state_licenses":           fixtureLicense(101) + "," + fixtureLicense(202),
			}
			if !tc.omit {
				settings["ec2_state_exporter_urls"] = tc.value
			}
			raw := fixtureScrape("# TYPE aws_ec2_instance_state gauge\naws_ec2_instance_state{instance_id=\"i-0123456789abcdef0\",state_name=\"pending\",region=\"ap-northeast-2\"} 0\n")
			raw.TargetURL = "https://already-registered.invalid:19443/custom/metrics?region=test"
			for _, p := range requireForwardCount(t, new(Forwarder), settings, raw, 2) {
				if p.Id != 1002 || p.GetLong("stateCode") != 0 || p.GetString("stateName") != "pending" {
					t.Fatal("default code value zero must be a pending observation")
				}
			}
		})
	}
}

func TestForwarderMalformedExplicitSourcesFailClosed(t *testing.T) {
	const target = "https://registered.invalid/metrics?region=test"
	for _, configured := range []string{",", " , 	, ", target + ",", "," + target, target + ", ,https://other.invalid/metrics"} {
		t.Run(configured, func(t *testing.T) {
			settings := map[string]string{
				"ec2_state_forwarding_enabled": "true",
				"ec2_state_licenses":           fixtureLicense(101),
			}
			raw := fixtureScrape("aws_ec2_instance_state{instance_id=\"i-0123456789abcdef0\",state_name=\"running\"} 16\n")
			raw.TargetURL = target
			f := new(Forwarder)
			requireForwardCount(t, f, settings, raw, 1)
			running := raw.RawData
			raw.RawData = "aws_ec2_instance_state{instance_id=\"i-0123456789abcdef0\",state_name=\"terminated\"} 48\n"
			settings["ec2_state_exporter_urls"] = configured
			calls := 0
			n, err := f.Forward(settings, raw, -123, func(_ int64, _ int64, _ *pack.ParamPack) error {
				calls++
				return nil
			})
			if n != 0 || calls != 0 || err == nil || !strings.HasPrefix(err.Error(), "EC2_STATE_CONFIG:") {
				t.Fatalf("malformed explicit scope: accepted=%d calls=%d err=%v", n, calls, err)
			}
			delete(settings, "ec2_state_exporter_urls")
			raw.RawData = running
			requireForwardCount(t, f, settings, raw, 0) // Invalid config cannot reset history.
		})
	}
}

func TestForwarderExplicitSourcesKeepExactURLRestriction(t *testing.T) {
	const allowed = "https://registered.invalid:19443/custom/metrics?region=test"
	settings := map[string]string{
		"ec2_state_forwarding_enabled": "true",
		"ec2_state_licenses":           fixtureLicense(101),
		"ec2_state_exporter_urls":      " " + allowed + " , https://second.invalid/metrics ",
	}
	for _, tc := range []struct {
		target string
		want   int
	}{
		{allowed, 1},
		{"https://second.invalid/metrics", 1},
		{"http://registered.invalid:19443/custom/metrics?region=test", 0},
		{"https://registered.invalid:19443/custom/metrics?region=other", 0},
		{"https://registered.invalid:19443/custom/metrics", 0},
		{"https://registered.invalid:19443/custom/metrics/?region=test", 0},
		{"https://unselected.invalid:19443/custom/metrics?region=test", 0},
	} {
		t.Run(tc.target, func(t *testing.T) {
			raw := fixtureScrape("aws_ec2_instance_state{instance_id=\"i-0123456789abcdef0\",state_name=\"running\"} 16\n")
			raw.TargetURL = tc.target
			requireForwardCount(t, new(Forwarder), settings, raw, tc.want)
		})
	}
}
