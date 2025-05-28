package model

import (
	"github.com/whatap/golib/io"
	"github.com/whatap/golib/lang/pack"
	"github.com/whatap/golib/util/compressutil"
)

// Pack type constants for OpenMetric packs
const (
	PACK_OPEN_MX      = 0x6001
	PACK_OPEN_MX_HELP = 0x6002
)

// Write serializes a Label to a DataOutputX
func (l *Label) Write(o *io.DataOutputX) {
	o.WriteText(l.Key)
	o.WriteText(l.Value)
}

// Read deserializes a Label from a DataInputX
func (l *Label) Read(in *io.DataInputX) *Label {
	l.Key = in.ReadText()
	l.Value = in.ReadText()
	return l
}

// Write serializes an OpenMx to a DataOutputX
func (om *OpenMx) Write(o *io.DataOutputX) {
	o.WriteByte(0) // version
	o.WriteText(om.Metric)

	labelSize := 0
	if om.Labels != nil {
		labelSize = len(om.Labels)
	}
	o.WriteByte(byte(labelSize))

	for i := 0; i < labelSize; i++ {
		om.Labels[i].Write(o)
	}

	o.WriteLong(om.Timestamp)
	o.WriteDouble(om.Value)
}

// Read deserializes an OpenMx from a DataInputX
func (om *OpenMx) Read(in *io.DataInputX) *OpenMx {
	_ = in.ReadByte() // version
	om.Metric = in.ReadText()

	cnt := int(in.ReadByte())
	if cnt > 0 {
		om.Labels = make([]Label, cnt)
		for i := 0; i < cnt; i++ {
			om.Labels[i] = *new(Label).Read(in)
		}
	}

	om.Timestamp = in.ReadLong()
	om.Value = in.ReadDouble()

	return om
}

// Write serializes an OpenMxHelp to a DataOutputX
func (omh *OpenMxHelp) Write(o *io.DataOutputX) {
	o.WriteByte(0) // version
	o.WriteText(omh.Metric)

	// Write properties as key-value pairs
	o.WriteShort(int16(len(omh.Property)))
	for k, v := range omh.Property {
		o.WriteText(k)
		o.WriteText(v)
	}
}

// Read deserializes an OpenMxHelp from a DataInputX
func (omh *OpenMxHelp) Read(in *io.DataInputX) *OpenMxHelp {
	_ = in.ReadByte() // version
	omh.Metric = in.ReadText()

	// Read properties
	propCount := int(in.ReadShort())
	omh.Property = make(map[string]string, propCount)
	for i := 0; i < propCount; i++ {
		key := in.ReadText()
		value := in.ReadText()
		omh.Property[key] = value
	}

	return omh
}

// OpenMxPack represents a pack of OpenMx records for sending to the server
type OpenMxPack struct {
	pack.AbstractPack
	zip     byte
	bytes   []byte
	records []*OpenMx
}

// GetPackType returns the pack type
func (p *OpenMxPack) GetPackType() int16 {
	return PACK_OPEN_MX
}

// Write serializes the pack to a DataOutputX
func (p *OpenMxPack) Write(dout *io.DataOutputX) {
	p.AbstractPack.Write(dout)
	if p.bytes == nil {
		p.reset(p.records)
	}
	dout.WriteByte(p.zip)
	dout.WriteBlob(p.bytes)
}

// Read deserializes the pack from a DataInputX
func (p *OpenMxPack) Read(din *io.DataInputX) {
	p.AbstractPack.Read(din)
	p.zip = din.ReadByte()
	p.bytes = din.ReadBlob()
}

// SetRecords sets the records for the pack
func (p *OpenMxPack) SetRecords(items []*OpenMx) *OpenMxPack {
	p.records = items
	return p.reset(items)
}

// reset resets the pack with the given records
func (p *OpenMxPack) reset(items []*OpenMx) *OpenMxPack {
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
func (p *OpenMxPack) GetRecords() []*OpenMx {
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

	p.records = make([]*OpenMx, 0)
	_ = in.ReadByte() // version
	size := int(in.ReadShort())

	for i := 0; i < size; i++ {
		p.records = append(p.records, new(OpenMx).Read(in))
	}

	return p.records
}

// OpenMxHelpPack represents a pack of OpenMxHelp records for sending to the server
type OpenMxHelpPack struct {
	pack.AbstractPack
	zip     byte
	bytes   []byte
	records []*OpenMxHelp
}

// GetPackType returns the pack type
func (p *OpenMxHelpPack) GetPackType() int16 {
	return PACK_OPEN_MX_HELP
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

// NewOpenMxPack creates a new OpenMxPack
func NewOpenMxPack() *OpenMxPack {
	return &OpenMxPack{}
}

// NewOpenMxHelpPack creates a new OpenMxHelpPack
func NewOpenMxHelpPack() *OpenMxHelpPack {
	return &OpenMxHelpPack{}
}
