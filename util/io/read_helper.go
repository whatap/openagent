package io

import (
	"fmt"
	"net"

	"github.com/whatap/golib/io"
)

type NetReadHelper struct {
	tcp net.Conn
}

func NewNetReadHelper(tcp net.Conn) *NetReadHelper {
	in := new(NetReadHelper)
	in.tcp = tcp
	return in
}

func (this *NetReadHelper) ReadBytes(sz int) ([]byte, error) {
	buf, err := NewByteArray(int32(sz))
	if err != nil {
		return nil, err
	}
	nbytesleft := sz
	nbytesuntilnow := 0
	for nbytesleft > 0 {
		nbytethistime, err := this.tcp.Read(buf[nbytesuntilnow:])
		if err != nil {
			return nil, err
		}
		nbytesleft -= nbytethistime
		nbytesuntilnow += nbytethistime
	}

	return buf, err
}

func (in *NetReadHelper) ReadByte() (byte, error) {
	if b, err := in.ReadBytes(1); err == nil {
		return b[0], nil
	} else {
		return 0, err
	}
}
func (in *NetReadHelper) ReadInt() (int32, error) {
	if b, err := in.ReadBytes(4); err == nil {
		return io.ToInt(b, 0), nil
	} else {
		return 0, err
	}
}
func (in *NetReadHelper) ReadLong() (int64, error) {
	if b, err := in.ReadBytes(8); err == nil {
		return io.ToLong(b, 0), nil
	} else {
		return 0, err
	}
}

func (in *NetReadHelper) ReadIntBytesLimit(max int) ([]byte, error) {
	sz, err := in.ReadInt()
	if err != nil {
		return nil, err
	}
	if sz < 0 || sz > int32(max) {
		return nil, fmt.Errorf("ReadIntBytesLimit max reached ")
	}
	return in.ReadBytes(int(sz))
}

func (in *NetReadHelper) ReadShort() (int16, error) {
	b, err := in.ReadBytes(2)
	if b != nil {
		return io.ToShort(b, 0), nil
	}
	return 0, err
}
