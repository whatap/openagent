package model

import (
	"reflect"
	"testing"

	"github.com/whatap/golib/io"
)

// sampleHistogram returns a fully-populated OpenMxHistogram for round-trip tests.
func sampleHistogram() *OpenMxHistogram {
	h := NewOpenMxHistogram("http_request_duration_seconds", 1718668800000)
	h.AddLabel("service", "checkout")
	h.AddLabel("method", "POST")
	h.Data = NativeHistogramData{
		Schema:        3,
		ZeroThreshold: 1e-9,
		ZeroCount:     2,
		Count:         42,
		Sum:           123.456,
		PositiveSpans: []BucketSpan{
			{Offset: 0, Length: 3},
			{Offset: 2, Length: 1},
		},
		PositiveBuckets: []int64{5, -2, 1, 3}, // delta-encoded
		NegativeSpans: []BucketSpan{
			{Offset: -1, Length: 2},
		},
		NegativeBuckets: []int64{1, 4},
	}
	return h
}

func TestOpenMxHistogram_WriteRead(t *testing.T) {
	orig := sampleHistogram()

	o := io.NewDataOutputX()
	orig.Write(o)

	in := io.NewDataInputX(o.ToByteArray())
	got := new(OpenMxHistogram).Read(in)

	if !reflect.DeepEqual(orig, got) {
		t.Fatalf("round-trip mismatch:\n orig=%+v\n  got=%+v", orig, got)
	}
}

func TestOpenMxHistogram_EmptyBuckets(t *testing.T) {
	// A native histogram with no populated buckets must still round-trip its
	// scalar fields. Empty bucket arrays come back as empty (non-nil) slices.
	orig := NewOpenMxHistogram("empty_histo", 1718668800000)
	orig.Data = NativeHistogramData{
		Schema:        0,
		ZeroThreshold: 0,
		ZeroCount:     0,
		Count:         0,
		Sum:           0,
	}

	o := io.NewDataOutputX()
	orig.Write(o)
	got := new(OpenMxHistogram).Read(io.NewDataInputX(o.ToByteArray()))

	if got.Metric != orig.Metric || got.Timestamp != orig.Timestamp {
		t.Fatalf("identity mismatch: got metric=%q ts=%d", got.Metric, got.Timestamp)
	}
	if got.Data.Schema != 0 || got.Data.Count != 0 {
		t.Fatalf("scalar fields mismatch: %+v", got.Data)
	}
	if len(got.Data.PositiveBuckets) != 0 || len(got.Data.NegativeBuckets) != 0 {
		t.Fatalf("expected no buckets, got pos=%v neg=%v", got.Data.PositiveBuckets, got.Data.NegativeBuckets)
	}
	if len(got.Data.PositiveSpans) != 0 || len(got.Data.NegativeSpans) != 0 {
		t.Fatalf("expected no spans, got pos=%v neg=%v", got.Data.PositiveSpans, got.Data.NegativeSpans)
	}
}

func TestOpenMxHistogramPack_RoundTrip(t *testing.T) {
	records := []*OpenMxHistogram{sampleHistogram(), sampleHistogram(), sampleHistogram()}

	pk := NewOpenMxHistogramPack()
	pk.SetRecords(records)

	// Serialize the whole pack (exercises the compress path for >100 bytes)...
	o := io.NewDataOutputX()
	pk.Write(o)

	// ...and read it back through a fresh pack instance.
	decoded := NewOpenMxHistogramPack()
	decoded.Read(io.NewDataInputX(o.ToByteArray()))

	got := decoded.GetRecords()
	if len(got) != len(records) {
		t.Fatalf("record count mismatch: want %d, got %d", len(records), len(got))
	}
	for i := range records {
		if !reflect.DeepEqual(records[i], got[i]) {
			t.Fatalf("record %d mismatch:\n want=%+v\n  got=%+v", i, records[i], got[i])
		}
	}
}

func TestOpenMxHistogramPack_PackType(t *testing.T) {
	if got := NewOpenMxHistogramPack().GetPackType(); got != OPEN_MX_HISTOGRAM_PACK {
		t.Fatalf("pack type = 0x%x, want 0x%x", got, OPEN_MX_HISTOGRAM_PACK)
	}
	// Must not collide with the existing scalar / help pack types.
	if OPEN_MX_HISTOGRAM_PACK == OPEN_MX_PACK || OPEN_MX_HISTOGRAM_PACK == OPEN_MX_HELP_PACK {
		t.Fatalf("pack type collision: histo=0x%x mx=0x%x help=0x%x",
			OPEN_MX_HISTOGRAM_PACK, OPEN_MX_PACK, OPEN_MX_HELP_PACK)
	}
}
