package proxy

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/whatap/golib/config"
	"github.com/whatap/golib/lang/pack"
	"github.com/whatap/golib/logger"

	wio "github.com/whatap/golib/io"
	wnet "github.com/whatap/golib/net"
)

type TcpProxy struct {
	localSocket   net.Listener
	acceptSockets []net.Conn
	conf          *config.Config
	lock          sync.Mutex
	lastTime      int64
}

func NewTcpProxy(opts ...TcpProxyOption) *TcpProxy {
	p := new(TcpProxy)

	for _, opt := range opts {
		opt.apply(conf)
	}

	if conf.RelayTcpClient == nil {
		conf.RelayTcpClient = &wnet.EmptyTcpClient{}
	}
	if conf.Log == nil {
		conf.Log = &logger.EmptyLogger{}
	}
	return p
}

func (this *TcpProxy) Listen() (err error) {
	defer func() {
		if r := recover(); r != nil {
			conf.Log.Println("WA120004", "Recover ", r)
			this.Close()
		}
	}()

	// socket이 이미 있는 경우 다시 열려고 하면 모두 종료 후 다시 열기
	if this.localSocket != nil {
		this.Close()
	}

	this.localSocket, err = net.Listen("tcp", fmt.Sprintf(":%d", conf.Port))
	if err != nil {
		conf.Log.Println("WA120005", "Error: Unable to create socket. port=", conf.Port, " ", err)
		this.localSocket = nil
		return err
	} else {
		conf.Log.Println("WA120005-1", "Open TCP Listener Port:", conf.Port)
	}

	for {
		select {
		case <-conf.ctx.Done():
			break
		default:
			SockFileDescriptor, err := this.localSocket.Accept()
			if err != nil {
				//logutil.Println("ADDIN_telegraf004", "Unable to accept incoming messages over the socket. Error", err)
				//return err
				panic(fmt.Sprintf("WA120006 Unable to accept incoming messages over the socket. Error=%s", err))

			}
			this.acceptSockets = append(this.acceptSockets, SockFileDescriptor)
			//logutil.Println("debug", "Accpet", SockFileDescriptor)
			go this.processSocketRead(SockFileDescriptor)

		}
	}
}

func (this *TcpProxy) processSocketRead(c net.Conn) {
	defer func() {
		if r := recover(); r != nil {
			conf.Log.Println("WA120007", " Recover ", r)
			c.Close()
		}
	}()

	for {
		select {
		case <-conf.ctx.Done():
			break
		default:
			c.SetReadDeadline(time.Now().Add(time.Duration(conf.TcpSoTimeout) * time.Millisecond))
			dio := wio.NewDataInputNet(c)
			p := pack.ReadPack(dio)
			conf.RelayTcpClient.Send(p)
		}
	}
}

func (this *TcpProxy) Close() {
	if this.localSocket != nil {
		this.localSocket.Close()
		this.localSocket = nil
	}
	sockets := this.acceptSockets
	this.acceptSockets = make([]net.Conn, 0)
	for _, it := range sockets {
		if it != nil {
			it.Close()
		}
	}
	sockets = nil
}
func (this *TcpProxy) Destroy() {
	conf.cancel()
	this.Close()
}
