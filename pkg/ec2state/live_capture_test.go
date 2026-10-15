package ec2state

import (
	"bytes"
	"math"
	"os"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/whatap/golib/lang/pack"
	"github.com/whatap/golib/lang/value"
	"github.com/whatap/golib/util/hash"

	"open-agent/pkg/model"
)

// This is an opt-in replay of an externally captured /metrics response, not a
// live AWS query or collector/Yard test. Never check real captures into the repo.
func TestForwarderLiveCaptureWithMinimalSettings(t *testing.T) {
	path := os.Getenv("EC2_STATE_CAPTURE_FILE")
	if path == "" {
		t.Skip("set EC2_STATE_CAPTURE_FILE to replay a real EC2 exporter capture")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("cannot read EC2_STATE_CAPTURE_FILE")
	}
	var parser expfmt.TextParser
	families, err := parser.TextToMetricFamilies(bytes.NewReader(body))
	if err != nil {
		t.Fatal("capture is not valid Prometheus text exposition")
	}
	family := families["aws_ec2_instance_state"]
	if family == nil || family.GetType() != dto.MetricType_GAUGE || len(family.Metric) == 0 {
		t.Fatal("capture has no canonical EC2 gauge samples")
	}
	type expectedState struct {
		code int64
		name string
	}
	expected := make(map[string]expectedState)
	// Test oracle is independent of production metric/label/state defaults.
	states := map[int64]string{0: "pending", 16: "running", 32: "shutting-down", 48: "terminated", 64: "stopping", 80: "stopped"}
	for _, metric := range family.Metric {
		labels := make(map[string]string)
		for _, label := range metric.Label {
			if _, exists := labels[label.GetName()]; exists {
				t.Fatal("capture contains duplicate labels")
			}
			labels[label.GetName()] = label.GetValue()
		}
		if metric.GetGauge() == nil || metric.GetGauge().Value == nil || metric.TimestampMs != nil {
			t.Fatal("capture must contain numeric gauge values without explicit timestamps")
		}
		sample := metric.GetGauge().GetValue()
		if math.IsNaN(sample) || math.IsInf(sample, 0) || math.Trunc(sample) != sample || sample < 0 || sample > 80 {
			t.Fatal("capture contains an invalid EC2 state code")
		}
		code := int64(sample)
		name, known := states[code]
		if !known || labels["state_name"] != name || labels["instance_id"] == "" || labels["region"] == "" {
			t.Fatal("capture does not satisfy the canonical state/identity contract")
		}
		if _, duplicate := expected[labels["instance_id"]]; duplicate {
			t.Fatal("capture contains duplicate instance identities")
		}
		expected[labels["instance_id"]] = expectedState{code: code, name: name}
	}

	licenses := []string{fixtureLicense(101), fixtureLicense(202)}
	settings := map[string]string{
		"ec2_state_forwarding_enabled": "true",
		"ec2_state_licenses":           licenses[0] + "," + licenses[1],
	}
	routes := map[int64]int64{101: hash.Hash64Str(licenses[0]), 202: hash.Hash64Str(licenses[1])}
	raw := &model.ScrapeRawData{
		TargetURL:      "https://captured-ec2-exporter.invalid/metrics",
		ContentType:    "text/plain; version=0.0.4",
		CollectionTime: time.Now().UnixMilli(), // Replay time; not an AWS observation timestamp.
		RawData:        string(body),           // Feed actual bytes unchanged; no sample substitution.
	}
	type delivery struct {
		pcode      int64
		instanceID string
	}
	seen := make(map[delivery]bool)
	f := new(Forwarder)
	n, err := f.Forward(settings, raw, -123, func(pcode, licenseHash int64, p *pack.ParamPack) error {
		wantHash, knownRoute := routes[pcode]
		if !knownRoute || licenseHash != wantHash {
			t.Fatal("capture fanout reached an incorrect synthetic-license route")
		}
		decoded, ok := pack.ToPack(pack.ToBytesPack(p)).(*pack.ParamPack)
		if !ok || decoded.Id != 1002 || decoded.Pcode != pcode || decoded.Oid != -123 || decoded.Time != raw.CollectionTime || decoded.Request != 0 || decoded.Response != 0 {
			t.Fatal("capture lost its command1002/oneway/destination envelope")
		}
		id, idOK := decoded.Get("instanceId").(*value.TextValue)
		name, nameOK := decoded.Get("stateName").(*value.TextValue)
		code, codeOK := decoded.Get("stateCode").(*value.DecimalValue)
		if !idOK || !nameOK || !codeOK {
			t.Fatal("capture payload lost its text/integer wire types")
		}
		want, exists := expected[id.Val]
		if !exists || want.code != code.Val || want.name != name.Val {
			t.Fatal("capture payload differs from the actual exporter sample")
		}
		keys, count := decoded.Keys(), 0
		for keys.HasMoreElements() {
			key := keys.NextString()
			if key != "instanceId" && key != "stateCode" && key != "stateName" {
				t.Fatal("unexpected capture payload field")
			}
			count++
		}
		if count != 3 {
			t.Fatal("capture payload must contain exactly three business fields")
		}
		key := delivery{pcode: pcode, instanceID: id.Val}
		if seen[key] {
			t.Fatal("capture delivered a duplicate instance/project pair")
		}
		seen[key] = true
		return nil // No real sender, collector or network connection.
	})
	want := len(expected) * len(routes)
	if err != nil || n != want || len(seen) != want {
		t.Fatalf("capture samples=%d accepted=%d distinct deliveries=%d want=%d err=%v", len(expected), n, len(seen), want, err)
	}
	for pcode := range routes {
		for instanceID := range expected {
			if !seen[delivery{pcode: pcode, instanceID: instanceID}] {
				t.Fatal("capture omitted an instance/project pair")
			}
		}
	}
	requireForwardCount(t, f, settings, raw, 0)
	t.Logf("replayed real capture: samples=%d synthetic destinations=%d typed command1002 packs=%d; no network sends", len(expected), len(routes), n)
}
