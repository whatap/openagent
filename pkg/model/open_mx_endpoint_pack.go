package model

import (
	"github.com/whatap/golib/io"
	"github.com/whatap/golib/lang/pack"
)

// Pack type constant for OpenMxEndpointPack
const (
	OPEN_MX_ENDPOINT_PACK = 0x1606
)

// OpenMxEndpointPack represents a pack of endpoint paths for sending to the server
type OpenMxEndpointPack struct {
	pack.AbstractPack
	Endpoints []string
}

// GetPackType returns the pack type
func (p *OpenMxEndpointPack) GetPackType() int16 {
	return OPEN_MX_ENDPOINT_PACK
}

// Write serializes the pack to a DataOutputX
func (p *OpenMxEndpointPack) Write(dout *io.DataOutputX) {
	p.AbstractPack.Write(dout)
	if p.Endpoints == nil {
		dout.WriteShort(0)
	} else {
		dout.WriteShort(int16(len(p.Endpoints)))
		for _, ep := range p.Endpoints {
			dout.WriteText(ep)
		}
	}
}

// Read deserializes the pack from a DataInputX
func (p *OpenMxEndpointPack) Read(din *io.DataInputX) {
	p.AbstractPack.Read(din)
	size := int(din.ReadShort())
	p.Endpoints = make([]string, 0, size)
	for i := 0; i < size; i++ {
		p.Endpoints = append(p.Endpoints, din.ReadText())
	}
}

// NewOpenMxEndpointPack creates a new OpenMxEndpointPack
func NewOpenMxEndpointPack() *OpenMxEndpointPack {
	return &OpenMxEndpointPack{}
}
