package model

import (
	"github.com/whatap/golib/io"
	"github.com/whatap/golib/lang/pack"
	"github.com/whatap/golib/util/compressutil"
)

// Pack type constant for OpenMxHistogramPack.
//
// 0x1607 is the value agreed with the WhaTap collection-server team (KAZAA-593):
// the original 0x1605 proposal collides with TAG_META in the server-side
// PackEnum, so 0x1607 was confirmed against apm-server PackEnum.java.
const (
	OPEN_MX_HISTOGRAM_PACK = 0x1607
)

// OpenMxHistogramPack represents a pack of OpenMxHistogram records for sending
// to the server. It mirrors OpenMxPack so the existing send/compress plumbing
// applies unchanged, but uses a distinct pack type so the server can route
// native histograms to a dedicated handler without touching the scalar path.
type OpenMxHistogramPack struct {
	pack.AbstractPack
	zip     byte
	bytes   []byte
	records []*OpenMxHistogram
}

// GetPackType returns the pack type.
func (p *OpenMxHistogramPack) GetPackType() int16 {
	return OPEN_MX_HISTOGRAM_PACK
}

// Write serializes the pack to a DataOutputX.
func (p *OpenMxHistogramPack) Write(dout *io.DataOutputX) {
	p.AbstractPack.Write(dout)
	if p.bytes == nil {
		p.reset(p.records)
	}
	dout.WriteByte(p.zip)
	dout.WriteBlob(p.bytes)
}

// Read deserializes the pack from a DataInputX.
func (p *OpenMxHistogramPack) Read(din *io.DataInputX) {
	p.AbstractPack.Read(din)
	p.zip = din.ReadByte()
	p.bytes = din.ReadBlob()
}

// SetRecords sets the records for the pack.
func (p *OpenMxHistogramPack) SetRecords(items []*OpenMxHistogram) *OpenMxHistogramPack {
	p.records = items
	return p.reset(items)
}

// reset resets the pack with the given records.
func (p *OpenMxHistogramPack) reset(items []*OpenMxHistogram) *OpenMxHistogramPack {
	o := io.NewDataOutputX()
	o.WriteByte(0) // version

	if items == nil {
		o.WriteShort(0)
	} else {
		o.WriteShort(int16(len(items)))
		for i := 0; i < len(items); i++ {
			items[i].Write(o)
		}
	}

	p.bytes = o.ToByteArray()
	if len(p.bytes) > 100 {
		p.zip = 1
		compressed, err := compressutil.DoZip(p.bytes)
		if err == nil {
			p.bytes = compressed
		}
	}

	return p
}

// GetRecords returns the records from the pack.
func (p *OpenMxHistogramPack) GetRecords() []*OpenMxHistogram {
	if p.bytes == nil {
		return nil
	}

	var in *io.DataInputX
	if p.zip == 1 {
		unzipped, err := compressutil.UnZip(p.bytes)
		if err != nil {
			return nil
		}
		in = io.NewDataInputX(unzipped)
	} else {
		in = io.NewDataInputX(p.bytes)
	}

	p.records = make([]*OpenMxHistogram, 0)
	_ = in.ReadByte() // version
	size := int(in.ReadShort())

	for i := 0; i < size; i++ {
		p.records = append(p.records, new(OpenMxHistogram).Read(in))
	}

	return p.records
}

// NewOpenMxHistogramPack creates a new OpenMxHistogramPack.
func NewOpenMxHistogramPack() *OpenMxHistogramPack {
	return &OpenMxHistogramPack{}
}
