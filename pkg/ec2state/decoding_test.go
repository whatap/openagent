package ec2state

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/whatap/golib/lang/pack"
	"google.golang.org/protobuf/proto"
)

const protobufContentType = "application/vnd.google.protobuf; proto=io.prometheus.client.MetricFamily; encoding=delimited"

func protobufState(t *testing.T) string {
	t.Helper()
	family := &dto.MetricFamily{
		Name: proto.String("fixture_ec2_state"), Type: dto.MetricType_GAUGE.Enum(),
		Metric: []*dto.Metric{{
			Gauge: &dto.Gauge{Value: proto.Float64(1)},
			Label: []*dto.LabelPair{
				{Name: proto.String("instanceId"), Value: proto.String("i-0123456789abcdef0")},
				{Name: proto.String("stateCode"), Value: proto.String("48")},
				{Name: proto.String("stateName"), Value: proto.String("terminated")},
			},
		}},
	}
	var body bytes.Buffer
	if err := expfmt.NewEncoder(&body, expfmt.NewFormat(expfmt.TypeProtoDelim)).Encode(family); err != nil {
		t.Fatal(err)
	}
	return body.String()
}

func TestForwardSupportsNegotiatedProtobuf(t *testing.T) {
	raw := fixtureScrape(protobufState(t))
	raw.ContentType = protobufContentType
	n, err := new(Forwarder).Forward(fixtureSettings(), raw, -123, func(_ int64, _ int64, p *pack.ParamPack) error {
		if p.GetLong("stateCode") != 48 || p.GetString("stateName") != "terminated" {
			t.Fatal("wrong protobuf observation")
		}
		return nil
	})
	if n != 2 || err != nil {
		t.Fatalf("accepted=%d err=%v", n, err)
	}
}

func TestForwardBoundsInputAndValidatesContentType(t *testing.T) {
	var oversizedLength [binary.MaxVarintLen64]byte
	size := binary.PutUvarint(oversizedLength[:], 1<<32)
	for _, tc := range []struct{ name, contentType, body string }{
		{"oversize body", "text/plain", validStateBody + "# " + strings.Repeat("x", 2*1024*1024) + "\n"},
		{"unknown content type", "application/json", validStateBody},
		{"wrong protobuf schema", "application/vnd.google.protobuf; proto=wrong; encoding=delimited", protobufState(t)},
		{"truncated protobuf", protobufContentType, protobufState(t) + "\x10\x01"},
		{"huge length prefix", protobufContentType, string(oversizedLength[:size])},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := fixtureScrape(tc.body)
			raw.ContentType = tc.contentType
			calls := 0
			n, err := new(Forwarder).Forward(fixtureSettings(), raw, -123, func(_ int64, _ int64, _ *pack.ParamPack) error { calls++; return nil })
			if n != 0 || calls != 0 || err == nil {
				t.Fatalf("accepted=%d calls=%d hasError=%v", n, calls, err != nil)
			}
		})
	}
}

func TestForwardRejectsIncompleteProtobufSampleBeforeSending(t *testing.T) {
	for _, invalid := range []*dto.Metric{
		{},
		{Gauge: &dto.Gauge{}},
	} {
		families, err := decodeProtobuf([]byte(protobufState(t)))
		if err != nil {
			t.Fatal(err)
		}
		family := families["fixture_ec2_state"]
		family.Metric = append(family.Metric, invalid)
		var body bytes.Buffer
		if err := expfmt.NewEncoder(&body, expfmt.NewFormat(expfmt.TypeProtoDelim)).Encode(family); err != nil {
			t.Fatal(err)
		}
		raw := fixtureScrape(body.String())
		raw.ContentType = protobufContentType
		calls := 0
		n, err := new(Forwarder).Forward(fixtureSettings(), raw, -123, func(_ int64, _ int64, _ *pack.ParamPack) error { calls++; return nil })
		if n != 0 || calls != 0 || err == nil {
			t.Fatalf("accepted=%d calls=%d hasError=%v", n, calls, err != nil)
		}
	}
}
