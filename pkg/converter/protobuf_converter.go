package converter

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"

	configPkg "open-agent/pkg/config"
	"open-agent/pkg/model"
	"open-agent/tools/util/logutil"
)

// ProtobufContentType is the base media type used by the Prometheus protobuf
// exposition format (delimited io.prometheus.client.MetricFamily messages).
const ProtobufContentType = "application/vnd.google.protobuf"

// IsProtobufContentType reports whether the given Content-Type response header
// indicates a Prometheus protobuf (delimited MetricFamily) payload.
func IsProtobufContentType(contentType string) bool {
	if contentType == "" {
		return false
	}
	h := http.Header{}
	h.Set("Content-Type", contentType)
	return expfmt.ResponseFormat(h).FormatType() == expfmt.TypeProtoDelim
}

// ConvertWithContentType converts a scraped payload to OpenMx format, selecting
// the protobuf or text decoder based on the response Content-Type.
//
// Text (and any non-protobuf) payloads fall back to the existing line-based
// parser so classic exposition collection is completely unaffected.
func ConvertWithContentType(data []byte, contentType string, collectionTime int64) (*model.ConversionResult, error) {
	if IsProtobufContentType(contentType) {
		return ConvertProtobufWithTimestamp(data, collectionTime)
	}
	return ConvertWithTimestamp(string(data), collectionTime)
}

// ConvertProtobuf decodes a delimited Prometheus protobuf payload into OpenMx
// using the current time as the collection timestamp.
func ConvertProtobuf(data []byte) (*model.ConversionResult, error) {
	return ConvertProtobufWithTimestamp(data, time.Now().UnixMilli())
}

// ConvertProtobufWithTimestamp decodes a delimited Prometheus protobuf payload
// (Content-Type: application/vnd.google.protobuf; encoding=delimited) into the
// OpenMx representation.
//
// Classic metric types (counter, gauge, summary, untyped and classic
// histograms with explicit buckets) are converted to exactly the same flat
// series the text parser produces, so switching a target to protobuf does not
// change collected classic metrics.
//
// Native (sparse) histograms are converted into model.OpenMxHistogram records
// (the schema confirmed in KAZAA-592 — Option B, a sibling type that leaves the
// scalar OpenMx wire format untouched) and returned on the result's
// OpenMxHistogramList. The confirmed schema carries integer, delta-encoded
// buckets and uint64 counts, so standard (counter-derived) integer native
// histograms convert losslessly. Float native histograms (gauge histograms and
// values produced by float operations, which carry absolute float bucket
// counts) cannot be represented by that schema and are skipped with a count
// (see the summary log below). If a target exposes classic buckets alongside a
// native histogram, those classic series are still collected normally.
func ConvertProtobufWithTimestamp(data []byte, collectionTime int64) (*model.ConversionResult, error) {
	openMxList := make([]*model.OpenMx, 0)
	histogramList := make([]*model.OpenMxHistogram, 0)
	helpMap := make(map[string]*model.OpenMxHelp)

	dec := expfmt.NewDecoder(bytes.NewReader(data), expfmt.NewFormat(expfmt.TypeProtoDelim))
	nativeHistogramCount := 0
	floatNativeSkipped := 0

	for {
		var mf dto.MetricFamily
		err := dec.Decode(&mf)
		if err == io.EOF {
			break
		}
		if err != nil {
			// Surface the error but keep the families decoded so far: a malformed
			// tail should not silently discard valid leading metrics.
			partial := model.NewConversionResult(openMxList, helpMapToSlice(helpMap))
			partial.SetOpenMxHistogramList(histogramList)
			return partial, fmt.Errorf("error decoding protobuf metric family: %v", err)
		}

		name := mf.GetName()
		if name == "" {
			continue
		}

		// HELP / TYPE metadata, mirroring the text parser's helpMap.
		omh := model.NewOpenMxHelp(name)
		if mf.Help != nil {
			omh.Put("help", mf.GetHelp())
		}
		omh.Put("type", metricTypeString(mf.GetType()))
		helpMap[name] = omh

		for _, m := range mf.GetMetric() {
			if m == nil {
				continue
			}

			ts := collectionTime
			if m.TimestampMs != nil && m.GetTimestampMs() > 0 {
				ts = m.GetTimestampMs()
			}
			labels := m.GetLabel()

			switch mf.GetType() {
			case dto.MetricType_COUNTER:
				if c := m.GetCounter(); c != nil {
					openMxList = append(openMxList, newOpenMxWithLabels(name, ts, c.GetValue(), labels))
				}
			case dto.MetricType_GAUGE:
				if g := m.GetGauge(); g != nil {
					openMxList = append(openMxList, newOpenMxWithLabels(name, ts, g.GetValue(), labels))
				}
			case dto.MetricType_UNTYPED:
				if u := m.GetUntyped(); u != nil {
					openMxList = append(openMxList, newOpenMxWithLabels(name, ts, u.GetValue(), labels))
				}
			case dto.MetricType_SUMMARY:
				if s := m.GetSummary(); s != nil {
					openMxList = append(openMxList, convertSummary(name, ts, s, labels)...)
				}
			case dto.MetricType_HISTOGRAM, dto.MetricType_GAUGE_HISTOGRAM:
				h := m.GetHistogram()
				if h == nil {
					continue
				}
				// Classic buckets first — preserves exact flat-series behavior.
				series, _ := convertClassicHistogram(name, ts, h, labels)
				openMxList = append(openMxList, series...)

				// Native histogram: convert into the dedicated OpenMxHistogram
				// structure (KAZAA-592 schema). Classic buckets (above) and the
				// native histogram are independent representations of the same
				// metric, so a target exposing both yields both forms.
				if isNativeHistogram(h) {
					nativeHistogramCount++
					if omh, ok := convertNativeHistogram(name, ts, h, labels); ok {
						histogramList = append(histogramList, omh)
					} else {
						// Float native histogram: outside the confirmed integer
						// schema. Skipped, but counted and surfaced below.
						floatNativeSkipped++
						if configPkg.IsDebugEnabled() {
							logutil.Debugf("CONVERTER",
								"[CONVERTER] Float native histogram %q (schema=%d) skipped: not representable by the integer OpenMxHistogram schema (KAZAA-592)",
								name, h.GetSchema())
						}
					}
				}
			}
		}
	}

	if floatNativeSkipped > 0 {
		logutil.Infof("CONVERTER",
			"Converted %d native histogram metric(s) to OpenMxHistogram; %d float native histogram(s) skipped (not representable by the integer OpenMxHistogram schema, KAZAA-592). Classic buckets, where exposed alongside, were collected normally.",
			nativeHistogramCount-floatNativeSkipped, floatNativeSkipped)
	} else if nativeHistogramCount > 0 {
		logutil.Debugf("CONVERTER",
			"Converted %d native histogram metric(s) to OpenMxHistogram.", nativeHistogramCount)
	}

	result := model.NewConversionResult(openMxList, helpMapToSlice(helpMap))
	result.SetOpenMxHistogramList(histogramList)
	return result, nil
}

// convertSummary expands a summary into its flat series: one series per quantile
// plus _sum and _count, identical to the text exposition.
func convertSummary(name string, ts int64, s *dto.Summary, labels []*dto.LabelPair) []*model.OpenMx {
	series := make([]*model.OpenMx, 0, len(s.GetQuantile())+2)
	for _, q := range s.GetQuantile() {
		if q == nil {
			continue
		}
		om := newOpenMxWithLabels(name, ts, q.GetValue(), labels)
		om.AddLabel("quantile", strconv.FormatFloat(q.GetQuantile(), 'g', -1, 64))
		series = append(series, om)
	}
	series = append(series, newOpenMxWithLabels(name+"_sum", ts, s.GetSampleSum(), labels))
	series = append(series, newOpenMxWithLabels(name+"_count", ts, float64(s.GetSampleCount()), labels))
	return series
}

// convertClassicHistogram expands the explicit buckets of a classic histogram
// into flat _bucket{le=...} series plus _sum and _count. It returns whether any
// classic series were emitted (false for a native-only histogram with no
// explicit buckets). The le="+Inf" bucket is synthesized when absent so the
// output matches the text exposition, which always includes it.
func convertClassicHistogram(name string, ts int64, h *dto.Histogram, labels []*dto.LabelPair) ([]*model.OpenMx, bool) {
	buckets := h.GetBucket()
	if len(buckets) == 0 {
		return nil, false
	}

	series := make([]*model.OpenMx, 0, len(buckets)+3)
	hasInf := false
	for _, b := range buckets {
		if b == nil {
			continue
		}
		ub := b.GetUpperBound()
		if math.IsInf(ub, +1) {
			hasInf = true
		}
		count := float64(b.GetCumulativeCount())
		if b.CumulativeCountFloat != nil && b.GetCumulativeCountFloat() > 0 {
			count = b.GetCumulativeCountFloat()
		}
		om := newOpenMxWithLabels(name+"_bucket", ts, count, labels)
		om.AddLabel("le", formatLE(ub))
		series = append(series, om)
	}

	sampleCount := histogramSampleCount(h)
	if !hasInf {
		om := newOpenMxWithLabels(name+"_bucket", ts, sampleCount, labels)
		om.AddLabel("le", "+Inf")
		series = append(series, om)
	}
	series = append(series, newOpenMxWithLabels(name+"_sum", ts, h.GetSampleSum(), labels))
	series = append(series, newOpenMxWithLabels(name+"_count", ts, sampleCount, labels))
	return series, true
}

// isNativeHistogram reports whether the protobuf histogram carries native
// (sparse, exponential) histogram data.
func isNativeHistogram(h *dto.Histogram) bool {
	return h.Schema != nil || len(h.GetPositiveSpan()) > 0 || len(h.GetNegativeSpan()) > 0
}

// isFloatNativeHistogram reports whether the native histogram carries absolute
// float bucket counts (PositiveCount/NegativeCount, *Float counts) instead of
// integer, delta-encoded buckets. These arise from gauge histograms and float
// operations and cannot be represented by the integer OpenMxHistogram schema
// (KAZAA-592), so the converter skips them rather than emitting lossy data.
func isFloatNativeHistogram(h *dto.Histogram) bool {
	return len(h.GetPositiveCount()) > 0 || len(h.GetNegativeCount()) > 0 ||
		h.SampleCountFloat != nil || h.ZeroCountFloat != nil
}

// convertNativeHistogram converts a protobuf native (sparse) histogram into the
// OpenMxHistogram structure confirmed in KAZAA-592. It returns ok=false for
// float native histograms, which the integer schema cannot represent.
//
// Bucket counts are carried verbatim as the protobuf delta encoding (the first
// entry of each span list is an absolute count, each subsequent entry is the
// delta from its predecessor); the schema and OpenMxHistogram both preserve
// this encoding, so no decode/re-encode of the bucket deltas is performed here.
func convertNativeHistogram(name string, ts int64, h *dto.Histogram, labels []*dto.LabelPair) (*model.OpenMxHistogram, bool) {
	if isFloatNativeHistogram(h) {
		return nil, false
	}

	omh := model.NewOpenMxHistogram(name, ts)
	for _, l := range labels {
		if l == nil {
			continue
		}
		omh.AddLabel(l.GetName(), l.GetValue())
	}

	omh.Data = model.NativeHistogramData{
		Schema:          h.GetSchema(),
		ZeroThreshold:   h.GetZeroThreshold(),
		ZeroCount:       h.GetZeroCount(),
		Count:           h.GetSampleCount(),
		Sum:             h.GetSampleSum(),
		PositiveSpans:   convertBucketSpans(h.GetPositiveSpan()),
		PositiveBuckets: copyInt64Slice(h.GetPositiveDelta()),
		NegativeSpans:   convertBucketSpans(h.GetNegativeSpan()),
		NegativeBuckets: copyInt64Slice(h.GetNegativeDelta()),
	}
	return omh, true
}

// convertBucketSpans maps protobuf BucketSpans to the model representation.
func convertBucketSpans(spans []*dto.BucketSpan) []model.BucketSpan {
	if len(spans) == 0 {
		return nil
	}
	out := make([]model.BucketSpan, 0, len(spans))
	for _, s := range spans {
		if s == nil {
			continue
		}
		out = append(out, model.BucketSpan{Offset: s.GetOffset(), Length: s.GetLength()})
	}
	return out
}

// copyInt64Slice returns a defensive copy so the OpenMxHistogram does not alias
// memory owned by the protobuf decoder.
func copyInt64Slice(in []int64) []int64 {
	if len(in) == 0 {
		return nil
	}
	out := make([]int64, len(in))
	copy(out, in)
	return out
}

// histogramSampleCount returns the total sample count, preferring the float
// field when present (used by native/float histograms).
func histogramSampleCount(h *dto.Histogram) float64 {
	if h.SampleCountFloat != nil && h.GetSampleCountFloat() > 0 {
		return h.GetSampleCountFloat()
	}
	return float64(h.GetSampleCount())
}

// newOpenMxWithLabels builds an OpenMx with the supplied protobuf label pairs.
func newOpenMxWithLabels(metric string, ts int64, value float64, labels []*dto.LabelPair) *model.OpenMx {
	om := model.NewOpenMx(metric, ts, value)
	for _, l := range labels {
		if l == nil {
			continue
		}
		om.AddLabel(l.GetName(), l.GetValue())
	}
	return om
}

// formatLE renders a histogram bucket upper bound as an `le` label value,
// matching the Prometheus text exposition (shortest round-trippable form, with
// "+Inf" for the catch-all bucket).
func formatLE(upperBound float64) string {
	if math.IsInf(upperBound, +1) {
		return "+Inf"
	}
	return strconv.FormatFloat(upperBound, 'g', -1, 64)
}

// metricTypeString maps a protobuf metric type to the lowercase string used in
// the text exposition's TYPE line (and stored in OpenMxHelp).
func metricTypeString(t dto.MetricType) string {
	switch t {
	case dto.MetricType_COUNTER:
		return "counter"
	case dto.MetricType_GAUGE:
		return "gauge"
	case dto.MetricType_SUMMARY:
		return "summary"
	case dto.MetricType_HISTOGRAM:
		return "histogram"
	case dto.MetricType_GAUGE_HISTOGRAM:
		return "gaugehistogram"
	case dto.MetricType_UNTYPED:
		return "untyped"
	default:
		return "untyped"
	}
}

// helpMapToSlice flattens the help map, mirroring the text converter.
func helpMapToSlice(helpMap map[string]*model.OpenMxHelp) []*model.OpenMxHelp {
	out := make([]*model.OpenMxHelp, 0, len(helpMap))
	for _, omh := range helpMap {
		out = append(out, omh)
	}
	return out
}
