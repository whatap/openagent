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
// OpenMx flat-series representation.
//
// Classic metric types (counter, gauge, summary, untyped and classic
// histograms with explicit buckets) are converted to exactly the same flat
// series the text parser produces, so switching a target to protobuf does not
// change collected classic metrics.
//
// Native (sparse) histograms are structurally decoded here — the Schema, zero
// bucket, positive/negative spans and deltas are all parsed by the protobuf
// decoder — but their conversion into the native OpenMx structure is deferred to
// KAZAA-591 step 4, which depends on the OpenMx schema design in KAZAA-592. For
// now native histogram series are skipped (their classic buckets, if the target
// exposes them alongside, are still collected normally).
func ConvertProtobufWithTimestamp(data []byte, collectionTime int64) (*model.ConversionResult, error) {
	openMxList := make([]*model.OpenMx, 0)
	helpMap := make(map[string]*model.OpenMxHelp)

	dec := expfmt.NewDecoder(bytes.NewReader(data), expfmt.NewFormat(expfmt.TypeProtoDelim))
	nativeHistogramCount := 0

	for {
		var mf dto.MetricFamily
		err := dec.Decode(&mf)
		if err == io.EOF {
			break
		}
		if err != nil {
			// Surface the error but keep the families decoded so far: a malformed
			// tail should not silently discard valid leading metrics.
			return model.NewConversionResult(openMxList, helpMapToSlice(helpMap)),
				fmt.Errorf("error decoding protobuf metric family: %v", err)
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
				series, classicEmitted := convertClassicHistogram(name, ts, h, labels)
				openMxList = append(openMxList, series...)

				// Native histogram: decoded but emission deferred (see doc comment).
				if isNativeHistogram(h) {
					nativeHistogramCount++
					if !classicEmitted && configPkg.IsDebugEnabled() {
						logutil.Debugf("CONVERTER",
							"[CONVERTER] Native histogram %q decoded (schema=%d) but OpenMx emission deferred (KAZAA-591 step 4)",
							name, h.GetSchema())
					}
				}
			}
		}
	}

	if nativeHistogramCount > 0 {
		logutil.Infof("CONVERTER",
			"Decoded %d native histogram metric(s) from protobuf; native OpenMx conversion is pending (KAZAA-591 step 4 / KAZAA-592). Classic buckets, where exposed alongside, were collected normally.",
			nativeHistogramCount)
	}

	return model.NewConversionResult(openMxList, helpMapToSlice(helpMap)), nil
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
