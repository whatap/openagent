package model

import (
	"github.com/whatap/golib/io"
	"github.com/whatap/golib/lang/pack"
	"github.com/whatap/golib/util/compressutil"
)

// Pack type constant for OpenMxHelpPack
const (
	OPEN_MX_HELP_PACK = 0x1604
)

// OpenMxHelpPack represents a pack of OpenMxHelp records for sending to the server
type OpenMxHelpPack struct {
	pack.AbstractPack
	zip     byte
	bytes   []byte
	records []*OpenMxHelp
}

// GetPackType returns the pack type
func (p *OpenMxHelpPack) GetPackType() int16 {
	return OPEN_MX_HELP_PACK
}

// Write serializes the pack to a DataOutputX
func (p *OpenMxHelpPack) Write(dout *io.DataOutputX) {
	p.AbstractPack.Write(dout)
	if p.bytes == nil {
		p.reset(p.records)
	}
	dout.WriteByte(p.zip)
	dout.WriteBlob(p.bytes)
}

// Read deserializes the pack from a DataInputX
func (p *OpenMxHelpPack) Read(din *io.DataInputX) {
	p.AbstractPack.Read(din)
	p.zip = din.ReadByte()
	p.bytes = din.ReadBlob()
}

// SetRecords sets the records for the pack
func (p *OpenMxHelpPack) SetRecords(items []*OpenMxHelp) *OpenMxHelpPack {
	p.records = items
	return p.reset(items)
}

// reset resets the pack with the given records
func (p *OpenMxHelpPack) reset(items []*OpenMxHelp) *OpenMxHelpPack {
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

// GetRecords returns the records from the pack
func (p *OpenMxHelpPack) GetRecords() []*OpenMxHelp {
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

	p.records = make([]*OpenMxHelp, 0)
	_ = in.ReadByte() // version
	size := int(in.ReadShort())

	for i := 0; i < size; i++ {
		p.records = append(p.records, new(OpenMxHelp).Read(in))
	}

	return p.records
}

// NewOpenMxHelpPack creates a new OpenMxHelpPack
func NewOpenMxHelpPack() *OpenMxHelpPack {
	return &OpenMxHelpPack{}
}
