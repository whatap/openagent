package common

import (
	"fmt"
	"github.com/whatap/gointernal/net/secure"
	"github.com/whatap/golib/io"
	"github.com/whatap/golib/lang/pack"
	"github.com/whatap/golib/util/compressutil"
	"time"
)

// Pack type constant for TagData
const (
	PACK_TAG_DATA = 0x6003
)

// TagData represents a collection of tags
type TagData struct {
	pack.AbstractPack
	zip   byte
	bytes []byte
	tags  map[string]string
}

// NewTagData creates a new TagData instance
func NewTagData() *TagData {
	return &TagData{
		tags: make(map[string]string),
	}
}

// AddTag adds a tag to the TagData
func (td *TagData) AddTag(key, value string) {
	td.tags[key] = value
}

// GetTag retrieves a tag from the TagData
func (td *TagData) GetTag(key string) string {
	return td.tags[key]
}

// GetPackType returns the pack type
func (td *TagData) GetPackType() int16 {
	return PACK_TAG_DATA
}

// Write serializes the TagData to a DataOutputX
func (td *TagData) Write(dout *io.DataOutputX) {
	td.AbstractPack.Write(dout)
	if td.bytes == nil {
		td.reset()
	}
	dout.WriteByte(td.zip)
	dout.WriteBlob(td.bytes)
}

// Read deserializes the TagData from a DataInputX
func (td *TagData) Read(din *io.DataInputX) {
	td.AbstractPack.Read(din)
	td.zip = din.ReadByte()
	td.bytes = din.ReadBlob()
	td.parseTags()
}

// reset resets the TagData
func (td *TagData) reset() *TagData {
	o := io.NewDataOutputX()
	o.WriteByte(0) // version

	// Write the number of tags
	o.WriteShort(int16(len(td.tags)))

	// Write each tag as a key-value pair
	for k, v := range td.tags {
		o.WriteText(k)
		o.WriteText(v)
	}

	td.bytes = o.ToByteArray()
	if len(td.bytes) > 100 {
		td.zip = 1
		compressed, err := compressutil.DoZip(td.bytes)
		if err == nil {
			td.bytes = compressed
		}
	}

	return td
}

// parseTags parses the tags from the bytes
func (td *TagData) parseTags() {
	if td.bytes == nil {
		return
	}

	var in *io.DataInputX
	if td.zip == 1 {
		unzipped, err := compressutil.UnZip(td.bytes)
		if err != nil {
			return
		}
		in = io.NewDataInputX(unzipped)
	} else {
		in = io.NewDataInputX(td.bytes)
	}

	td.tags = make(map[string]string)
	_ = in.ReadByte() // version
	size := int(in.ReadShort())

	for i := 0; i < size; i++ {
		key := in.ReadText()
		value := in.ReadText()
		td.tags[key] = value
	}
}

// sendTagData sends the TagData to the server
func (td *TagData) SendTagData(pcode int64) {
	// Set the time to the current time
	td.SetTime(time.Now().UnixMilli())

	// Get the security master from the secure package
	securityMaster := secure.GetSecurityMaster()
	if securityMaster == nil {
		fmt.Println("No security master available")
		return
	}

	// Set the PCODE and OID from the security master
	td.SetPCODE(pcode)
	td.SetOID(securityMaster.OID)

	// pack
	tagPack := pack.NewTagCountPack()
	tagPack.Category = "open_metric_test"
	tagPack.Time = td.GetTime()
	tagPack.SetPCODE(td.GetPCODE())
	tagPack.Oid = securityMaster.OID

	for k, v := range td.tags {
		tagPack.Put(k, v)
	}
	// Send the pack to the server using secure.Send
	secure.Send(secure.NET_SECURE_HIDE, tagPack, true)
}
