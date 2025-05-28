package proxy

import (
	"testing"

	_ "github.com/stretchr/testify/assert"
)

func TestTcpProxyWithSecure(t *testing.T) {
	/*
		wInfo := wnet.NewWhatapTcpServerInfo("x4mdi20rc4u11-z70j9leunenf0i-x1tlb4ui3648pi", "13.124.11.223/13.209.172.35", "", "", "")
		client := secure.GetSecureTcpClient(secure.WithWhatapTcpServer(wInfo), secure.WithLogger(logger.NewDefaultLogger()))
		client.Connect()

		relay := NewTcpProxy(WithRelayTcpClient(client))
		go relay.Listen()

		p := pack.NewLogSinkPack()
		sm := secure.GetSecurityMaster()
		p.Pcode = sm.PCODE
		p.Oid = sm.OID
	*/
}
