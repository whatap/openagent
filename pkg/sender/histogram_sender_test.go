package sender

import (
	"bytes"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/whatap/golib/lang/pack"
	"google.golang.org/protobuf/proto"

	"open-agent/pkg/converter"
	"open-agent/pkg/model"
)

func TestSendHistogramsChunksAndUsesHistogramPack(t *testing.T) {
	s := NewSender(make(chan *model.ConversionResult), nil, false)
	var sent []pack.Pack
	s.sendToServerFn = func(p pack.Pack) error {
		sent = append(sent, p)
		return nil
	}

	histograms := make([]*model.OpenMxHistogram, ChunkSize+1)
	for i := range histograms {
		histograms[i] = model.NewOpenMxHistogram("native_histogram", int64(i))
	}
	s.sendHistograms(histograms)

	if len(sent) != 2 {
		t.Fatalf("sent pack count = %d, want 2", len(sent))
	}
	wantSizes := []int{ChunkSize, 1}
	for i, p := range sent {
		histogramPack, ok := p.(*model.OpenMxHistogramPack)
		if !ok {
			t.Fatalf("sent pack %d type = %T, want *model.OpenMxHistogramPack", i, p)
		}
		if got := len(histogramPack.GetRecords()); got != wantSizes[i] {
			t.Errorf("sent pack %d record count = %d, want %d", i, got, wantSizes[i])
		}
	}
}

func TestIntegerNativeHistogramReachesServerSend(t *testing.T) {
	native := &dto.MetricFamily{
		Name: proto.String("native_latency_seconds"),
		Type: dto.MetricType_HISTOGRAM.Enum(),
		Metric: []*dto.Metric{{Histogram: &dto.Histogram{
			SampleCount:   proto.Uint64(5),
			SampleSum:     proto.Float64(2.5),
			Schema:        proto.Int32(2),
			ZeroThreshold: proto.Float64(0.001),
			ZeroCount:     proto.Uint64(1),
			PositiveSpan: []*dto.BucketSpan{{
				Offset: proto.Int32(0), Length: proto.Uint32(1),
			}},
			PositiveDelta: []int64{4},
		}}},
	}
	var payload bytes.Buffer
	encoder := expfmt.NewEncoder(&payload, expfmt.NewFormat(expfmt.TypeProtoDelim))
	if err := encoder.Encode(native); err != nil {
		t.Fatalf("encode native histogram: %v", err)
	}
	result, err := converter.ConvertProtobufWithTimestamp(payload.Bytes(), 1_700_000_000_000)
	if err != nil {
		t.Fatalf("convert native histogram: %v", err)
	}

	s := NewSender(make(chan *model.ConversionResult), nil, false)
	var sent pack.Pack
	s.sendToServerFn = func(p pack.Pack) error {
		sent = p
		return nil
	}
	s.sendResult(result)

	histogramPack, ok := sent.(*model.OpenMxHistogramPack)
	if !ok {
		t.Fatalf("server send received %T, want *model.OpenMxHistogramPack", sent)
	}
	records := histogramPack.GetRecords()
	if len(records) != 1 || records[0].Metric != "native_latency_seconds" {
		t.Fatalf("sent histogram records = %+v, want native_latency_seconds", records)
	}
}
