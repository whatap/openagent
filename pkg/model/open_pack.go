package model

import (
	"github.com/whatap/golib/io"
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

