package model

import (
	"github.com/whatap/golib/io"
)

const (
	MapValueType  byte = 80
	nullValueType byte = 0
	textValueType byte = 50
)

// OpenMxHelp represents help information for a metric
type OpenMxHelp struct {
	Metric   string
	Property map[string]string
}

// NewOpenMxHelp creates a new OpenMxHelp instance
func NewOpenMxHelp(metric string) *OpenMxHelp {
	return &OpenMxHelp{
		Metric:   metric,
		Property: make(map[string]string),
	}
}

// Put adds a property to the help information
func (omh *OpenMxHelp) Put(key, value string) {
	omh.Property[key] = value
}

// Get retrieves a property from the help information
func (omh *OpenMxHelp) Get(key string) string {
	return omh.Property[key]
}

// Write serializes an OpenMxHelp to a DataOutputX
// This implementation is based on the Java implementation
func (omh *OpenMxHelp) Write(o *io.DataOutputX) {
	o.WriteByte(0) // version
	o.WriteText(omh.Metric)
	if omh.Property == nil {
		omh.Property = make(map[string]string)
	}

	// In Java, this would be o.writeValue(this.property)
	// We use our custom WriteValue function to achieve the same result
	WriteValue(o, omh.Property)
}

// Read deserializes an OpenMxHelp from a DataInputX
// This implementation is based on the Java implementation
func (omh *OpenMxHelp) Read(in *io.DataInputX) *OpenMxHelp {
	_ = in.ReadByte() // version
	omh.Metric = in.ReadText()

	// Read the value type
	valueType := in.ReadByte()
	if valueType != MapValueType {
		// If it's not a map value, return with an empty property map
		omh.Property = make(map[string]string)
		return omh
	}

	// Read the number of properties
	propCount := int(in.ReadDecimal())
	omh.Property = make(map[string]string, propCount)

	// Then read each property as a key-value pair
	for i := 0; i < propCount; i++ {
		key := in.ReadText()
		value := in.ReadText()
		omh.Property[key] = value
	}

	return omh
}
