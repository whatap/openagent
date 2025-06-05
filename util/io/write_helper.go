package io

import (
	"net"
	"time"
)

type NetWriteHelper struct {
	tcp net.Conn
}

func NewNetWriteHelper(tcp net.Conn) *NetWriteHelper {
	writer := new(NetWriteHelper)
	writer.tcp = tcp
	return writer
}

//WriteBytes WriteBytes
func (this *NetWriteHelper) WriteBytes(sendbuf []byte, timeout time.Duration) error {
	buflen := len(sendbuf)
	nbyteleft := buflen
	for 0 < nbyteleft {
		this.tcp.SetWriteDeadline(time.Now().Add(timeout))
		nbytethistime, err := this.tcp.Write(sendbuf[buflen-nbyteleft : buflen])
		if err != nil {
			this.tcp.Close()
			return err
		}
		nbyteleft -= nbytethistime
	}

	this.tcp.SetWriteDeadline(time.Time{})
	return nil
}
