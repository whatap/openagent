package secure

import (
	"testing"
)

func TestConnect(t *testing.T) {
	// wInfo := wnet.NewWhatapTcpServerInfo("x2tggtnopk2t9-z39dt59pe1pmjc-xipbnkb0ph6bn", "222.237.239.142", "6600", "", "")
	// tm := GetInstanceTcpManager(WithWhatapTcpServer(wInfo))
	// tm.StartNet()
	// client := GetTcpSession()

}

func TestInterface(t *testing.T) {
	/*
		var client wnet.TcpClient
		//wInfo := wnet.NewWhatapTcpServerInfo("x2e0g7rorn665-x6rdsu6t89f7ru-z1qe6c3jrdcsk5", "15.165.146.117", "6600", "", "")
		wInfo := wnet.NewWhatapTcpServerInfo("x4mdi20rc4u11-z70j9leunenf0i-x1tlb4ui3648pi", "13.124.11.223/13.209.172.35", "", "", "")
		client = GetSecureTcpClient(WithWhatapTcpServer(wInfo), WithLogger(logger.NewDefaultLogger()))
		client.Connect()
		fmt.Println("Connect")
		assert.Equal(t, GetTcpSession().isOpen(), true)
		client.Close()
	*/
}
