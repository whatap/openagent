package model

import (
	"github.com/whatap/golib/io"
)

// BucketSpan describes a contiguous run of populated buckets in a Prometheus
// Native (sparse) Histogram. It mirrors io.prometheus.client.BucketSpan:
//   - Offset is the gap (in bucket indexes) from the end of the previous span,
//     or from the implicit zero index for the first span. It may be negative.
//   - Length is the number of consecutive buckets covered by this span.
type BucketSpan struct {
	Offset int32
	Length uint32
}

// NativeHistogramData holds the sparse, exponential-bucket payload of a
// Prometheus Native Histogram. Bucket counts are delta-encoded int64s, carried
// verbatim from the protobuf exposition format (io.prometheus.client.Histogram):
// the first entry of each *Buckets slice is an absolute count and every later
// entry is the delta from its predecessor.
//
// A nil/zero-valued NativeHistogramData is meaningless on its own; it is always
// the payload of an OpenMxHistogram.
type NativeHistogramData struct {
	// Schema selects the bucket resolution. The bucket boundaries are
	// 2^(2^-Schema). Valid Prometheus values are currently -4..8.
	Schema int32
	// ZeroThreshold is the width of the zero bucket (observations whose
	// absolute value is <= ZeroThreshold fall into the zero bucket).
	ZeroThreshold float64
	// ZeroCount is the number of observations in the zero bucket.
	ZeroCount uint64
	// Count is the total number of observations across all buckets.
	Count uint64
	// Sum is the sum of all observed values.
	Sum float64

	// PositiveSpans / PositiveBuckets describe buckets for positive values.
	PositiveSpans   []BucketSpan
	PositiveBuckets []int64
	// NegativeSpans / NegativeBuckets describe buckets for negative values.
	NegativeSpans   []BucketSpan
	NegativeBuckets []int64
}

// OpenMxHistogram represents a single Native Histogram sample. It is a sibling
// of OpenMx (which stays scalar-only) so that the existing OpenMx wire format
// and the server's scalar decode path are left completely untouched. Native
// Histograms travel in their own OpenMxHistogramPack (see open_mx_histogram_pack.go).
//
// Classic histograms are NOT represented here: in Prometheus text/OpenMetrics
// exposition a classic histogram is already a set of plain scalar series
// (_bucket, _sum, _count) and continues to flow through OpenMx unchanged.
type OpenMxHistogram struct {
	Metric    string
	Timestamp int64
	Labels    []Label
	Data      NativeHistogramData
}

// NewOpenMxHistogram creates a new OpenMxHistogram with the given identity.
func NewOpenMxHistogram(metric string, timestamp int64) *OpenMxHistogram {
	return &OpenMxHistogram{
		Metric:    metric,
		Timestamp: timestamp,
		Labels:    make([]Label, 0),
	}
}

// AddLabel appends a label to the histogram sample.
func (h *OpenMxHistogram) AddLabel(key, value string) {
	h.Labels = append(h.Labels, Label{Key: key, Value: value})
}

// Write serializes an OpenMxHistogram to a DataOutputX. The leading version
// byte mirrors OpenMx/OpenMxHelp and allows forward-compatible field additions.
func (h *OpenMxHistogram) Write(o *io.DataOutputX) {
	o.WriteByte(0) // version
	o.WriteText(h.Metric)

	labelSize := len(h.Labels)
	o.WriteByte(byte(labelSize))
	for i := 0; i < labelSize; i++ {
		h.Labels[i].Write(o)
	}

	o.WriteLong(h.Timestamp)
	h.Data.write(o)
}

// Read deserializes an OpenMxHistogram from a DataInputX.
func (h *OpenMxHistogram) Read(in *io.DataInputX) *OpenMxHistogram {
	_ = in.ReadByte() // version
	h.Metric = in.ReadText()

	cnt := int(in.ReadByte())
	if cnt > 0 {
		h.Labels = make([]Label, cnt)
		for i := 0; i < cnt; i++ {
			h.Labels[i] = *new(Label).Read(in)
		}
	}

	h.Timestamp = in.ReadLong()
	h.Data.read(in)

	return h
}

// write serializes the native histogram payload.
func (d *NativeHistogramData) write(o *io.DataOutputX) {
	o.WriteInt(d.Schema)
	o.WriteDouble(d.ZeroThreshold)
	o.WriteLong(int64(d.ZeroCount))
	o.WriteLong(int64(d.Count))
	o.WriteDouble(d.Sum)

	writeBucketSpans(o, d.PositiveSpans)
	o.WriteLongArray(d.PositiveBuckets)
	writeBucketSpans(o, d.NegativeSpans)
	o.WriteLongArray(d.NegativeBuckets)
}

// read deserializes the native histogram payload.
func (d *NativeHistogramData) read(in *io.DataInputX) {
	d.Schema = in.ReadInt()
	d.ZeroThreshold = in.ReadDouble()
	d.ZeroCount = uint64(in.ReadLong())
	d.Count = uint64(in.ReadLong())
	d.Sum = in.ReadDouble()

	d.PositiveSpans = readBucketSpans(in)
	d.PositiveBuckets = in.ReadLongArray()
	d.NegativeSpans = readBucketSpans(in)
	d.NegativeBuckets = in.ReadLongArray()
}

// writeBucketSpans serializes a slice of BucketSpan. A short length prefix keeps
// it compact while allowing far more spans than a histogram realistically has.
func writeBucketSpans(o *io.DataOutputX, spans []BucketSpan) {
	o.WriteShort(int16(len(spans)))
	for i := range spans {
		o.WriteInt(spans[i].Offset)
		o.WriteInt(int32(spans[i].Length))
	}
}

// readBucketSpans deserializes a slice of BucketSpan.
func readBucketSpans(in *io.DataInputX) []BucketSpan {
	n := int(in.ReadShort())
	if n <= 0 {
		return nil
	}
	spans := make([]BucketSpan, n)
	for i := 0; i < n; i++ {
		spans[i].Offset = in.ReadInt()
		spans[i].Length = uint32(in.ReadInt())
	}
	return spans
}
