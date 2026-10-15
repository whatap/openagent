package ec2state

import (
	"fmt"
	"testing"

	"github.com/whatap/golib/lang/value"
)

func TestForwarderDefaultsToCanonicalExporterContract(t *testing.T) {
	settings := map[string]string{
		"ec2_state_forwarding_enabled": "true",
		"ec2_state_licenses":           fixtureLicense(101) + "," + fixtureLicense(202),
		"ec2_state_exporter_urls":      "https://registered-exporter.invalid/custom/metrics?region=test",
	}
	raw := fixtureScrape("# TYPE aws_ec2_instance_state gauge\naws_ec2_instance_state{instance_id=\"i-0123456789abcdef0\",state_name=\"terminated\",region=\"ap-northeast-2\"} 48\n")
	raw.TargetURL = "https://registered-exporter.invalid/custom/metrics?region=test"
	for i, p := range requireForwardCount(t, new(Forwarder), settings, raw, 2) {
		if p.Id != 1002 || p.Pcode != []int64{101, 202}[i] || p.Oid != -123 || p.Time != raw.CollectionTime || p.Request != 0 || p.Response != 0 {
			t.Fatal("canonical observation lost its default oneway envelope")
		}
		if p.GetString("instanceId") != "i-0123456789abcdef0" || p.GetLong("stateCode") != 48 || p.GetString("stateName") != "terminated" {
			t.Fatal("canonical exporter observation did not preserve the three-field payload")
		}
		keys, count := p.Keys(), 0
		for keys.HasMoreElements() {
			key := keys.NextString()
			if key != "instanceId" && key != "stateCode" && key != "stateName" {
				t.Fatalf("unexpected payload field %q", key)
			}
			count++
		}
		if count != 3 {
			t.Fatalf("payload fields=%d want=3", count)
		}
	}
}

func TestForwarderCanonicalMetricNameDefaultsPassAllStates(t *testing.T) {
	for _, metric := range []struct {
		name, setting string
		omit          bool
	}{
		{"omitted", "", true},
		{"empty", "", false},
		{"whitespace", " 	\n ", false},
		{"explicit canonical", "aws_ec2_instance_state", false},
		{"padded canonical", " aws_ec2_instance_state 	", false},
	} {
		t.Run(metric.name, func(t *testing.T) {
			for _, state := range []struct {
				code int64
				name string
			}{
				{0, "pending"}, {16, "running"}, {32, "shutting-down"},
				{48, "terminated"}, {64, "stopping"}, {80, "stopped"},
			} {
				t.Run(state.name, func(t *testing.T) {
					settings := map[string]string{
						"ec2_state_forwarding_enabled": "true",
						"ec2_state_licenses":           fixtureLicense(101) + "," + fixtureLicense(202),
					}
					if !metric.omit {
						settings["ec2_state_metric_name"] = metric.setting
					}
					// The literal metric, labels and numeric sample are independent of defaults.
					body := fmt.Sprintf("# TYPE aws_ec2_instance_state gauge\naws_ec2_instance_state{instance_id=\"i-0123456789abcdef0\",state_name=\"%s\",region=\"ap-northeast-2\"} %d\n", state.name, state.code)
					for _, p := range requireForwardCount(t, new(Forwarder), settings, fixtureScrape(body), 2) {
						code, ok := p.Get("stateCode").(*value.DecimalValue)
						if !ok || code.Val != state.code || p.GetString("stateName") != state.name || p.GetString("instanceId") != "i-0123456789abcdef0" {
							t.Fatal("canonical observation lost its state, identity or integer wire type")
						}
						for _, key := range []string{"instanceId", "stateName"} {
							if _, ok := p.Get(key).(*value.TextValue); !ok {
								t.Fatalf("%s lost its text wire type", key)
							}
						}
					}
				})
			}
		})
	}
}

func TestForwarderPreservesExplicitMetricAndLabelOverrides(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings map[string]string
		body     string
	}{
		{
			"canonical instance label override",
			map[string]string{"ec2_state_instance_id_label": "host_id"},
			"aws_ec2_instance_state{host_id=\"i-0123456789abcdef0\",instance_id=\"not-an-instance\",state_name=\"terminated\"} 48\n",
		},
		{
			"canonical name label override",
			map[string]string{"ec2_state_state_name_label": "status"},
			"aws_ec2_instance_state{instance_id=\"i-0123456789abcdef0\",status=\"terminated\",state_name=\"running\"} 48\n",
		},
		{
			"explicit canonical source override keeps default code label",
			map[string]string{"ec2_state_metric_name": "aws_ec2_instance_state", "ec2_state_state_code_source": "label"},
			"aws_ec2_instance_state{instance_id=\"i-0123456789abcdef0\",state_name=\"terminated\",stateCode=\"48\"} 1\n",
		},
		{
			"canonical code label override with label mode",
			map[string]string{"ec2_state_state_code_source": "label", "ec2_state_state_code_label": "aws_code"},
			"aws_ec2_instance_state{instance_id=\"i-0123456789abcdef0\",state_name=\"terminated\",aws_code=\"48\",stateCode=\"16\"} 1\n",
		},
		{
			"code label override does not switch default value mode",
			map[string]string{"ec2_state_state_code_label": "aws_code"},
			"aws_ec2_instance_state{instance_id=\"i-0123456789abcdef0\",state_name=\"terminated\",aws_code=\"16\"} 48\n",
		},
		{
			"custom metric keeps camelCase and label mode",
			map[string]string{"ec2_state_metric_name": "legacy_ec2_state"},
			"legacy_ec2_state{instanceId=\"i-0123456789abcdef0\",stateCode=\"48\",stateName=\"terminated\"} 1\naws_ec2_instance_state{instance_id=\"i-0123456789abcdef0\",state_name=\"running\"} 16\n",
		},
		{
			"custom metric keeps all label overrides",
			map[string]string{"ec2_state_metric_name": "legacy_ec2_state", "ec2_state_instance_id_label": "host", "ec2_state_state_name_label": "status", "ec2_state_state_code_label": "code"},
			"legacy_ec2_state{host=\"i-0123456789abcdef0\",code=\"48\",status=\"terminated\"} 1\n",
		},
		{
			"custom metric keeps explicit value source",
			map[string]string{"ec2_state_metric_name": "legacy_ec2_state", "ec2_state_state_code_source": "value"},
			"legacy_ec2_state{instanceId=\"i-0123456789abcdef0\",stateName=\"terminated\"} 48\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.settings["ec2_state_forwarding_enabled"] = "true"
			tc.settings["ec2_state_licenses"] = fixtureLicense(101)
			p := requireForwardCount(t, new(Forwarder), tc.settings, fixtureScrape(tc.body), 1)[0]
			if p.GetString("instanceId") != "i-0123456789abcdef0" || p.GetLong("stateCode") != 48 || p.GetString("stateName") != "terminated" {
				t.Fatal("explicit settings did not take precedence")
			}
		})
	}
}
