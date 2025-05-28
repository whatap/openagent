package proxy

import (
	"context"
	"time"

	"github.com/whatap/golib/config"
	"github.com/whatap/golib/logger"
	wnet "github.com/whatap/golib/net"
)

type tcpProxyConfig struct {
	Log            logger.Logger
	ctx            context.Context
	cancel         context.CancelFunc
	Port           int
	Timeout        time.Duration
	Config         config.Config
	ConfigObserver *config.ConfigObserver

	RelayTcpClient       wnet.TcpClient
	TcpSoTimeout         int
	TcpSoSendTimeout     int
	TcpConnectionTimeout int
}

type TcpProxyOption interface {
	apply(*tcpProxyConfig)
}
type funcTcpProxyOption struct {
	f func(*tcpProxyConfig)
}

func (this *funcTcpProxyOption) apply(c *tcpProxyConfig) {
	this.f(c)
}

func newFuncTcpProxyOption(f func(*tcpProxyConfig)) *funcTcpProxyOption {
	return &funcTcpProxyOption{
		f: f,
	}
}

const (
	defaultNetTimeout = 60
)

var (
	// default
	conf = &tcpProxyConfig{
		Port:                 6600,
		Timeout:              60,
		TcpSoTimeout:         120000,
		TcpSoSendTimeout:     20000,
		TcpConnectionTimeout: 5000,
	}
)

func WithContext(ctx context.Context, cancel context.CancelFunc) TcpProxyOption {
	return newFuncTcpProxyOption(func(c *tcpProxyConfig) {
		c.ctx = ctx
		c.cancel = cancel
	})
}
func WithLogger(logger logger.Logger) TcpProxyOption {
	return newFuncTcpProxyOption(func(c *tcpProxyConfig) {
		c.Log = logger
	})
}

func WithConfig(config config.Config) TcpProxyOption {
	return newFuncTcpProxyOption(func(c *tcpProxyConfig) {
		c.Config = config

	})
}

func WithConfigObserver(obj *config.ConfigObserver) TcpProxyOption {
	return newFuncTcpProxyOption(func(c *tcpProxyConfig) {
		c.ConfigObserver = obj
	})
}

func WithRelayTcpClient(client wnet.TcpClient) TcpProxyOption {
	return newFuncTcpProxyOption(func(c *tcpProxyConfig) {
		c.RelayTcpClient = client
	})
}
