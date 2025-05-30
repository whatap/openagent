package model

import (
	"github.com/whatap/golib/io"
)

// WriteValue writes a map of string to string as a MapValue
// This is a simplified version of the Java implementation of WriteValue
func WriteValue(o *io.DataOutputX, m map[string]string) {
	if m == nil {
		// If the map is nil, write the nullValueType
		o.WriteByte(nullValueType)
		return
	}
	// Write the value type (MapValue)
	o.WriteByte(MapValueType)

	// Write the number of entries in the map
	o.WriteDecimal(int64(len(m)))

	// Write each key-value pair
	for k, v := range m {
		o.WriteText(k)
		if v == "nil" {
			o.WriteByte(nullValueType)
		}
		o.WriteByte(textValueType)
		o.WriteText(v)
	}
}
