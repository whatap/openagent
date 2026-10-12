package ec2state

import (
	"bytes"
	"testing"

	"github.com/whatap/gointernal/lang/license"
	"github.com/whatap/golib/lang/pack"
	"github.com/whatap/golib/lang/value"

	"open-agent/pkg/model"
)

// This command is a test fixture, not an allocated Yard command.
const testCommandID = 190001

func fixtureLicense(pcode int64) string {
	return license.Build(pcode, bytes.Repeat([]byte{1}, 16))
}

func fixtureSettings() map[string]string {
	return map[string]string{
		"ec2_state_forwarding_enabled": "true",
		"ec2_state_exporter_urls":      "http://127.0.0.1:9106/metrics",
		"ec2_state_metric_name":        "fixture_ec2_state",
		"ec2_state_resend_interval":    "5m", // Explicit fixture interval.
		"ec2_state_param_id":           "190001",
		"ec2_state_licenses":           license.Build(101, bytes.Repeat([]byte{1}, 16)) + "," + license.Build(202, bytes.Repeat([]byte{2}, 16)),
	}
}

func fixtureScrape(body string) *model.ScrapeRawData {
	return &model.ScrapeRawData{
		TargetURL:      "http://127.0.0.1:9106/metrics",
		ContentType:    "text/plain; version=0.0.4",
		CollectionTime: 1700000000000,
		RawData:        body,
	}
}

func TestForwardSerializesParamPackForEachLicensedProject(t *testing.T) {
	settings := fixtureSettings()
	raw := fixtureScrape("# TYPE fixture_ec2_state gauge\nfixture_ec2_state{instanceId=\"i-0123456789abcdef0\",stateCode=\"48\",stateName=\"terminated\"} 1\n")
	var pcodes []int64
	n, err := new(Forwarder).Forward(settings, raw, -123, func(pcode, licenseHash int64, p *pack.ParamPack) error {
		pcodes = append(pcodes, pcode)
		decoded, ok := pack.ToPack(pack.ToBytesPack(p)).(*pack.ParamPack)
		if !ok {
			t.Fatal("not a serialized ParamPack")
		}
		if decoded.Pcode != pcode || decoded.Oid != -123 || decoded.Id != testCommandID || decoded.Time != raw.CollectionTime {
			t.Fatal("pack envelope does not preserve destination, sender, command, or observation time")
		}
		if decoded.Request != 0 || decoded.Response != 0 {
			t.Fatal("oneway observations must not request an ACK")
		}
		if decoded.GetString("instanceId") != "i-0123456789abcdef0" || decoded.GetLong("stateCode") != 48 || decoded.GetString("stateName") != "terminated" {
			t.Fatal("wrong instance-state payload")
		}
		if _, ok := decoded.Get("stateCode").(*value.DecimalValue); !ok {
			t.Fatal("stateCode is not a typed integer")
		}
		keys := decoded.Keys()
		for keys.HasMoreElements() {
			switch keys.NextString() {
			case "instanceId", "stateCode", "stateName":
			default:
				t.Fatal("unexpected business payload field")
			}
		}
		if licenseHash == 0 {
			t.Fatal("destination license hash missing")
		}
		return nil
	})
	if err != nil || n != 2 || len(pcodes) != 2 || pcodes[0] != 101 || pcodes[1] != 202 {
		t.Fatalf("forward result count=%d destinations=%v err=%v", n, pcodes, err)
	}
}

func TestForwardGatesUnconfiguredAndUnselectedSources(t *testing.T) {
	cases := []struct {
		name      string
		key       string
		setting   string
		wantError bool
	}{
		{"disabled", "ec2_state_forwarding_enabled", "false", false},
		{"default off", "ec2_state_forwarding_enabled", "", false},
		{"invalid enable", "ec2_state_forwarding_enabled", "maybe", true},
		{"zero command", "ec2_state_param_id", "0", true},
		{"negative command", "ec2_state_param_id", "-1", true},
		{"command overflow", "ec2_state_param_id", "2147483648", true},
		{"different exporter", "ec2_state_exporter_urls", "http://127.0.0.1:9107/metrics", false},
		{"missing exporter", "ec2_state_exporter_urls", "", true},
		{"missing metric", "ec2_state_metric_name", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			settings := fixtureSettings()
			settings[tc.key] = tc.setting
			calls := 0
			n, err := new(Forwarder).Forward(settings, fixtureScrape("fixture_ec2_state{instanceId=\"i-0123456789abcdef0\",stateCode=\"48\",stateName=\"terminated\"} 1\n"), -123, func(_ int64, _ int64, _ *pack.ParamPack) error {
				calls++
				return nil
			})
			if calls != 0 || n != 0 || (err != nil) != tc.wantError {
				t.Fatalf("calls=%d accepted=%d err=%v", calls, n, err)
			}
		})
	}
}
