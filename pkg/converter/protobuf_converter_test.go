package converter

import (
	"bytes"
	"math"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"google.golang.org/protobuf/proto"

	"open-agent/pkg/model"
)

const testTS int64 = 1_700_000_000_000

func inf() float64 { return math.Inf(1) }

// encodeDelimited encodes metric families as a delimited Prometheus protobuf
// payload (the wire format negotiated via application/vnd.google.protobuf).
func encodeDelimited(t *testing.T, families ...*dto.MetricFamily) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := expfmt.NewEncoder(&buf, expfmt.NewFormat(expfmt.TypeProtoDelim))
	for _, mf := range families {
		if err := enc.Encode(mf); err != nil {
			t.Fatalf("failed to encode metric family %q: %v", mf.GetName(), err)
		}
	}
	return buf.Bytes()
}

func labelPair(name, value string) *dto.LabelPair {
	return &dto.LabelPair{Name: proto.String(name), Value: proto.String(value)}
}

// findSeries returns OpenMx entries whose metric name matches and that carry the
// given label (key=value); pass an empty labelKey to ignore label filtering.
func findSeries(list []*model.OpenMx, metric, labelKey, labelVal string) []*model.OpenMx {
	var out []*model.OpenMx
	for _, om := range list {
		if om.Metric != metric {
			continue
		}
		if labelKey == "" {
			out = append(out, om)
			continue
		}
		for _, l := range om.Labels {
			if l.Key == labelKey && l.Value == labelVal {
				out = append(out, om)
				break
			}
		}
	}
	return out
}

func mustSingle(t *testing.T, list []*model.OpenMx, metric, labelKey, labelVal string) *model.OpenMx {
	t.Helper()
	got := findSeries(list, metric, labelKey, labelVal)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 series for %s{%s=%s}, got %d", metric, labelKey, labelVal, len(got))
	}
	return got[0]
}

func labelValue(om *model.OpenMx, key string) (string, bool) {
	for _, l := range om.Labels {
		if l.Key == key {
			return l.Value, true
		}
	}
	return "", false
}

func TestConvertProtobuf_CounterAndGauge(t *testing.T) {
	counter := &dto.MetricFamily{
		Name: proto.String("http_requests_total"),
		Help: proto.String("Total requests"),
		Type: dto.MetricType_COUNTER.Enum(),
		Metric: []*dto.Metric{{
			Label:   []*dto.LabelPair{labelPair("method", "get")},
			Counter: &dto.Counter{Value: proto.Float64(42)},
		}},
	}
	gauge := &dto.MetricFamily{
		Name: proto.String("temperature_celsius"),
		Type: dto.MetricType_GAUGE.Enum(),
		Metric: []*dto.Metric{{
			Gauge: &dto.Gauge{Value: proto.Float64(21.5)},
		}},
	}

	res, err := ConvertProtobufWithTimestamp(encodeDelimited(t, counter, gauge), testTS)
	if err != nil {
		t.Fatalf("ConvertProtobuf error: %v", err)
	}

	c := mustSingle(t, res.GetOpenMxList(), "http_requests_total", "method", "get")
	if c.Value != 42 {
		t.Errorf("counter value = %v, want 42", c.Value)
	}
	if c.Timestamp != testTS {
		t.Errorf("counter timestamp = %d, want %d", c.Timestamp, testTS)
	}

	g := mustSingle(t, res.GetOpenMxList(), "temperature_celsius", "", "")
	if g.Value != 21.5 {
		t.Errorf("gauge value = %v, want 21.5", g.Value)
	}

	// HELP/TYPE metadata parity.
	var counterHelp *model.OpenMxHelp
	for _, h := range res.GetOpenMxHelpList() {
		if h.Metric == "http_requests_total" {
			counterHelp = h
		}
	}
	if counterHelp == nil {
		t.Fatal("expected help entry for http_requests_total")
	}
	if got := counterHelp.Get("type"); got != "counter" {
		t.Errorf("type metadata = %q, want counter", got)
	}
	if got := counterHelp.Get("help"); got != "Total requests" {
		t.Errorf("help metadata = %q, want 'Total requests'", got)
	}
}

func TestConvertProtobuf_ClassicHistogram_WithExplicitInf(t *testing.T) {
	h := &dto.MetricFamily{
		Name: proto.String("request_duration_seconds"),
		Type: dto.MetricType_HISTOGRAM.Enum(),
		Metric: []*dto.Metric{{
			Label: []*dto.LabelPair{labelPair("path", "/api")},
			Histogram: &dto.Histogram{
				SampleCount: proto.Uint64(12),
				SampleSum:   proto.Float64(3.3),
				Bucket: []*dto.Bucket{
					{CumulativeCount: proto.Uint64(5), UpperBound: proto.Float64(0.1)},
					{CumulativeCount: proto.Uint64(9), UpperBound: proto.Float64(0.5)},
					{CumulativeCount: proto.Uint64(12), UpperBound: proto.Float64(inf())},
				},
			},
		}},
	}

	res, err := ConvertProtobufWithTimestamp(encodeDelimited(t, h), testTS)
	if err != nil {
		t.Fatalf("ConvertProtobuf error: %v", err)
	}
	list := res.GetOpenMxList()

	// Three buckets (incl. the explicit +Inf), one _sum, one _count.
	if got := len(findSeries(list, "request_duration_seconds_bucket", "", "")); got != 3 {
		t.Errorf("bucket series = %d, want 3 (no duplicate +Inf)", got)
	}
	b01 := mustSingle(t, list, "request_duration_seconds_bucket", "le", "0.1")
	if b01.Value != 5 {
		t.Errorf("le=0.1 bucket = %v, want 5", b01.Value)
	}
	if v, _ := labelValue(b01, "path"); v != "/api" {
		t.Errorf("bucket missing original label path=/api, got %q", v)
	}
	bInf := mustSingle(t, list, "request_duration_seconds_bucket", "le", "+Inf")
	if bInf.Value != 12 {
		t.Errorf("le=+Inf bucket = %v, want 12", bInf.Value)
	}
	sum := mustSingle(t, list, "request_duration_seconds_sum", "", "")
	if sum.Value != 3.3 {
		t.Errorf("_sum = %v, want 3.3", sum.Value)
	}
	count := mustSingle(t, list, "request_duration_seconds_count", "", "")
	if count.Value != 12 {
		t.Errorf("_count = %v, want 12", count.Value)
	}
}

func TestConvertProtobuf_ClassicHistogram_SynthesizesInf(t *testing.T) {
	h := &dto.MetricFamily{
		Name: proto.String("op_latency_seconds"),
		Type: dto.MetricType_HISTOGRAM.Enum(),
		Metric: []*dto.Metric{{
			Histogram: &dto.Histogram{
				SampleCount: proto.Uint64(7),
				SampleSum:   proto.Float64(1.1),
				Bucket: []*dto.Bucket{
					{CumulativeCount: proto.Uint64(3), UpperBound: proto.Float64(0.25)},
					{CumulativeCount: proto.Uint64(7), UpperBound: proto.Float64(1)},
				},
			},
		}},
	}

	res, err := ConvertProtobufWithTimestamp(encodeDelimited(t, h), testTS)
	if err != nil {
		t.Fatalf("ConvertProtobuf error: %v", err)
	}
	list := res.GetOpenMxList()

	// +Inf must be synthesized to match text exposition (== sample count).
	bInf := mustSingle(t, list, "op_latency_seconds_bucket", "le", "+Inf")
	if bInf.Value != 7 {
		t.Errorf("synthesized le=+Inf = %v, want 7", bInf.Value)
	}
	if got := len(findSeries(list, "op_latency_seconds_bucket", "", "")); got != 3 {
		t.Errorf("bucket series = %d, want 3 (2 explicit + synthesized +Inf)", got)
	}
}

func TestConvertProtobuf_Summary(t *testing.T) {
	s := &dto.MetricFamily{
		Name: proto.String("rpc_duration_seconds"),
		Type: dto.MetricType_SUMMARY.Enum(),
		Metric: []*dto.Metric{{
			Summary: &dto.Summary{
				SampleCount: proto.Uint64(100),
				SampleSum:   proto.Float64(25),
				Quantile: []*dto.Quantile{
					{Quantile: proto.Float64(0.5), Value: proto.Float64(0.2)},
					{Quantile: proto.Float64(0.99), Value: proto.Float64(0.9)},
				},
			},
		}},
	}

	res, err := ConvertProtobufWithTimestamp(encodeDelimited(t, s), testTS)
	if err != nil {
		t.Fatalf("ConvertProtobuf error: %v", err)
	}
	list := res.GetOpenMxList()

	q50 := mustSingle(t, list, "rpc_duration_seconds", "quantile", "0.5")
	if q50.Value != 0.2 {
		t.Errorf("quantile 0.5 = %v, want 0.2", q50.Value)
	}
	q99 := mustSingle(t, list, "rpc_duration_seconds", "quantile", "0.99")
	if q99.Value != 0.9 {
		t.Errorf("quantile 0.99 = %v, want 0.9", q99.Value)
	}
	if mustSingle(t, list, "rpc_duration_seconds_sum", "", "").Value != 25 {
		t.Error("_sum mismatch")
	}
	if mustSingle(t, list, "rpc_duration_seconds_count", "", "").Value != 100 {
		t.Error("_count mismatch")
	}
}

// findHistogram returns OpenMxHistogram records whose metric name matches.
func findHistogram(list []*model.OpenMxHistogram, metric string) []*model.OpenMxHistogram {
	var out []*model.OpenMxHistogram
	for _, h := range list {
		if h.Metric == metric {
			out = append(out, h)
		}
	}
	return out
}

func mustSingleHistogram(t *testing.T, list []*model.OpenMxHistogram, metric string) *model.OpenMxHistogram {
	t.Helper()
	got := findHistogram(list, metric)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 histogram for %s, got %d", metric, len(got))
	}
	return got[0]
}

func histogramLabel(h *model.OpenMxHistogram, key string) (string, bool) {
	for _, l := range h.Labels {
		if l.Key == key {
			return l.Value, true
		}
	}
	return "", false
}

// TestConvertProtobuf_NativeHistogram_IntegerConversion verifies step 4: an
// integer native (sparse) histogram is converted into an OpenMxHistogram with
// every field mapped per the KAZAA-592 schema, and that it does NOT leak into
// the flat OpenMx series list (native-only histogram → no classic series).
func TestConvertProtobuf_NativeHistogram_IntegerConversion(t *testing.T) {
	native := &dto.Histogram{
		SampleCount:   proto.Uint64(30),
		SampleSum:     proto.Float64(12.5),
		Schema:        proto.Int32(3),
		ZeroThreshold: proto.Float64(0.001),
		ZeroCount:     proto.Uint64(2),
		PositiveSpan: []*dto.BucketSpan{
			{Offset: proto.Int32(0), Length: proto.Uint32(2)},
		},
		PositiveDelta: []int64{4, -1},
		NegativeSpan: []*dto.BucketSpan{
			{Offset: proto.Int32(-1), Length: proto.Uint32(1)},
		},
		NegativeDelta: []int64{3},
	}
	mf := &dto.MetricFamily{
		Name:   proto.String("native_latency_seconds"),
		Type:   dto.MetricType_HISTOGRAM.Enum(),
		Metric: []*dto.Metric{{Label: []*dto.LabelPair{labelPair("path", "/api")}, Histogram: native}},
	}

	res, err := ConvertProtobufWithTimestamp(encodeDelimited(t, mf), testTS)
	if err != nil {
		t.Fatalf("ConvertProtobuf error: %v", err)
	}

	// Native-only histogram emits no flat OpenMx series.
	if got := len(res.GetOpenMxList()); got != 0 {
		t.Errorf("native-only histogram emitted %d flat series, want 0", got)
	}

	h := mustSingleHistogram(t, res.GetOpenMxHistogramList(), "native_latency_seconds")
	if h.Timestamp != testTS {
		t.Errorf("histogram timestamp = %d, want %d", h.Timestamp, testTS)
	}
	if v, _ := histogramLabel(h, "path"); v != "/api" {
		t.Errorf("histogram missing original label path=/api, got %q", v)
	}
	d := h.Data
	if d.Schema != 3 {
		t.Errorf("Schema = %d, want 3", d.Schema)
	}
	if d.ZeroThreshold != 0.001 {
		t.Errorf("ZeroThreshold = %v, want 0.001", d.ZeroThreshold)
	}
	if d.ZeroCount != 2 {
		t.Errorf("ZeroCount = %d, want 2", d.ZeroCount)
	}
	if d.Count != 30 {
		t.Errorf("Count = %d, want 30", d.Count)
	}
	if d.Sum != 12.5 {
		t.Errorf("Sum = %v, want 12.5", d.Sum)
	}
	if len(d.PositiveSpans) != 1 || d.PositiveSpans[0].Offset != 0 || d.PositiveSpans[0].Length != 2 {
		t.Errorf("PositiveSpans = %+v, want [{0 2}]", d.PositiveSpans)
	}
	if len(d.PositiveBuckets) != 2 || d.PositiveBuckets[0] != 4 || d.PositiveBuckets[1] != -1 {
		t.Errorf("PositiveBuckets = %v, want [4 -1] (delta-encoded, verbatim)", d.PositiveBuckets)
	}
	if len(d.NegativeSpans) != 1 || d.NegativeSpans[0].Offset != -1 || d.NegativeSpans[0].Length != 1 {
		t.Errorf("NegativeSpans = %+v, want [{-1 1}]", d.NegativeSpans)
	}
	if len(d.NegativeBuckets) != 1 || d.NegativeBuckets[0] != 3 {
		t.Errorf("NegativeBuckets = %v, want [3]", d.NegativeBuckets)
	}
}

// TestConvertProtobuf_DualHistogram_EmitsBoth verifies that a histogram exposing
// BOTH classic buckets and native fields yields the classic flat series (no
// regression) AND an OpenMxHistogram for the native data.
func TestConvertProtobuf_DualHistogram_EmitsBoth(t *testing.T) {
	dual := &dto.Histogram{
		SampleCount:   proto.Uint64(8),
		SampleSum:     proto.Float64(2.0),
		Schema:        proto.Int32(2),
		ZeroThreshold: proto.Float64(0.001),
		PositiveSpan:  []*dto.BucketSpan{{Offset: proto.Int32(0), Length: proto.Uint32(1)}},
		PositiveDelta: []int64{8},
		Bucket: []*dto.Bucket{
			{CumulativeCount: proto.Uint64(4), UpperBound: proto.Float64(0.5)},
			{CumulativeCount: proto.Uint64(8), UpperBound: proto.Float64(inf())},
		},
	}
	mf := &dto.MetricFamily{
		Name:   proto.String("dual_seconds"),
		Type:   dto.MetricType_HISTOGRAM.Enum(),
		Metric: []*dto.Metric{{Histogram: dual}},
	}

	res, err := ConvertProtobufWithTimestamp(encodeDelimited(t, mf), testTS)
	if err != nil {
		t.Fatalf("ConvertProtobuf error: %v", err)
	}
	list := res.GetOpenMxList()
	if got := len(findSeries(list, "dual_seconds_bucket", "", "")); got != 2 {
		t.Errorf("classic buckets = %d, want 2", got)
	}
	if mustSingle(t, list, "dual_seconds_count", "", "").Value != 8 {
		t.Error("dual histogram _count mismatch")
	}

	// Native data is also emitted as an OpenMxHistogram.
	h := mustSingleHistogram(t, res.GetOpenMxHistogramList(), "dual_seconds")
	if h.Data.Schema != 2 || h.Data.Count != 8 {
		t.Errorf("native part: schema=%d count=%d, want schema=2 count=8", h.Data.Schema, h.Data.Count)
	}
	if len(h.Data.PositiveBuckets) != 1 || h.Data.PositiveBuckets[0] != 8 {
		t.Errorf("native PositiveBuckets = %v, want [8]", h.Data.PositiveBuckets)
	}
}

// TestConvertProtobuf_FloatNativeHistogram_Skipped verifies that a float native
// histogram (absolute float bucket counts) is skipped — not representable by the
// integer OpenMxHistogram schema (KAZAA-592) — without emitting a histogram or
// erroring. A target exposing classic buckets alongside still gets those.
func TestConvertProtobuf_FloatNativeHistogram_Skipped(t *testing.T) {
	floatNative := &dto.Histogram{
		SampleCountFloat: proto.Float64(9.0),
		SampleSum:        proto.Float64(4.0),
		Schema:           proto.Int32(1),
		ZeroThreshold:    proto.Float64(0.001),
		ZeroCountFloat:   proto.Float64(1.0),
		PositiveSpan:     []*dto.BucketSpan{{Offset: proto.Int32(0), Length: proto.Uint32(2)}},
		PositiveCount:    []float64{3.0, 5.0}, // absolute float counts → float histogram
		Bucket: []*dto.Bucket{
			{CumulativeCount: proto.Uint64(9), UpperBound: proto.Float64(inf())},
		},
	}
	mf := &dto.MetricFamily{
		Name:   proto.String("float_native_seconds"),
		Type:   dto.MetricType_GAUGE_HISTOGRAM.Enum(),
		Metric: []*dto.Metric{{Histogram: floatNative}},
	}

	res, err := ConvertProtobufWithTimestamp(encodeDelimited(t, mf), testTS)
	if err != nil {
		t.Fatalf("ConvertProtobuf error: %v", err)
	}
	if got := len(findHistogram(res.GetOpenMxHistogramList(), "float_native_seconds")); got != 0 {
		t.Errorf("float native histogram emitted %d OpenMxHistogram(s), want 0 (skipped)", got)
	}
	// Classic +Inf bucket exposed alongside is still collected.
	if got := len(findSeries(res.GetOpenMxList(), "float_native_seconds_bucket", "", "")); got != 1 {
		t.Errorf("classic bucket alongside float native = %d, want 1", got)
	}
}

func TestConvertWithContentType_TextFallback(t *testing.T) {
	text := "# HELP foo A foo\n# TYPE foo counter\nfoo{a=\"b\"} 7\n"
	res, err := ConvertWithContentType([]byte(text), "text/plain; version=0.0.4", testTS)
	if err != nil {
		t.Fatalf("text fallback error: %v", err)
	}
	om := mustSingle(t, res.GetOpenMxList(), "foo", "a", "b")
	if om.Value != 7 {
		t.Errorf("text fallback value = %v, want 7", om.Value)
	}
}

func TestConvertWithContentType_ProtobufRouting(t *testing.T) {
	mf := &dto.MetricFamily{
		Name:   proto.String("up"),
		Type:   dto.MetricType_GAUGE.Enum(),
		Metric: []*dto.Metric{{Gauge: &dto.Gauge{Value: proto.Float64(1)}}},
	}
	ct := "application/vnd.google.protobuf;proto=io.prometheus.client.MetricFamily;encoding=delimited"
	res, err := ConvertWithContentType(encodeDelimited(t, mf), ct, testTS)
	if err != nil {
		t.Fatalf("protobuf routing error: %v", err)
	}
	if mustSingle(t, res.GetOpenMxList(), "up", "", "").Value != 1 {
		t.Error("protobuf-routed gauge mismatch")
	}
}

func TestIsProtobufContentType(t *testing.T) {
	cases := map[string]bool{
		"application/vnd.google.protobuf;proto=io.prometheus.client.MetricFamily;encoding=delimited": true,
		"application/vnd.google.protobuf; encoding=delimited":                                        true,
		"text/plain; version=0.0.4":                   false,
		"application/openmetrics-text; version=1.0.0": false,
		"": false,
	}
	for ct, want := range cases {
		if got := IsProtobufContentType(ct); got != want {
			t.Errorf("IsProtobufContentType(%q) = %v, want %v", ct, got, want)
		}
	}
}

func TestFormatLE(t *testing.T) {
	cases := map[float64]string{
		0.1:   "0.1",
		1:     "1",
		2.5:   "2.5",
		inf(): "+Inf",
		0.005: "0.005",
	}
	for in, want := range cases {
		if got := formatLE(in); got != want {
			t.Errorf("formatLE(%v) = %q, want %q", in, got, want)
		}
	}
}
