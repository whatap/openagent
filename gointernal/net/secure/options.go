package secure

import (
	"context"
	"time"

	"github.com/whatap/golib/config"
	"github.com/whatap/golib/logger"
	wnet "github.com/whatap/golib/net"
)

type tcpSessionConfig struct {
	Log            logger.Logger
	ctx            context.Context
	cancel         context.CancelFunc
	Timeout        time.Duration
	License        string
	AccessKey      string
	Servers        []string
	dest           int
	Pcode          int64
	Oid            int32
	UseQueue       bool
	Config         config.Config
	ConfigObserver *config.ConfigObserver

	ObjectName     string
	AppName        string
	AppProcessName string

	TcpSoTimeout         int32
	TcpSoSendTimeout     int32
	TcpConnectionTimeout int32

	NetWriteBufferSize int32
	NetSendMaxBytes    int32
	NetSendBufferSize  int32
	NetSendQueue1Size  int32
	NetSendQueue2Size  int32

	NetWriteLockEnabled bool

	CypherLevel  int32
	EncryptLevel int32

	QueueTcpEnabled           bool
	QueueLogEnabled           bool
	QueueTcpSenderThreadCount int32

	TimeSyncIntervalMs int64

	DebugTcpSendEnabled             bool
	DebugTcpSendPacks               []string
	DebugTcpFailoverEnabled         bool
	DebugTcpReadEnabled             bool
	DebugTcpSendTimeSyncEnabled     bool
	NetFailoverRetrySendDataEnabled bool

	MeterSelfEnabled bool
}

type TcpSessionOption interface {
	apply(*tcpSessionConfig)
}
type funcTcpSessionOption struct {
	f func(*tcpSessionConfig)
}

func (this *funcTcpSessionOption) apply(c *tcpSessionConfig) {
	this.f(c)
}

const (
	defaultNetTimeout = 60
)

var (
	// default
	conf = &tcpSessionConfig{
		Timeout:              60,
		TcpSoTimeout:         120000,
		TcpSoSendTimeout:     20000,
		TcpConnectionTimeout: 5000,
		ObjectName:           "{type}-{ip2}-{ip3}-{process}",

		NetWriteBufferSize:              8 * 1024 * 1024,
		NetSendMaxBytes:                 5 * 1024 * 1024,
		NetSendBufferSize:               1024,
		NetSendQueue1Size:               512,
		NetSendQueue2Size:               100000,
		NetWriteLockEnabled:             true,
		NetFailoverRetrySendDataEnabled: false,

		CypherLevel: 128,

		MeterSelfEnabled: true,

		QueueTcpEnabled:           true,
		QueueLogEnabled:           false,
		QueueTcpSenderThreadCount: 3,

		TimeSyncIntervalMs: 5000,

		DebugTcpSendEnabled:         false,
		DebugTcpFailoverEnabled:     false,
		DebugTcpReadEnabled:         false,
		DebugTcpSendTimeSyncEnabled: false,
	}
)

func newFuncTcpSessionOption(f func(*tcpSessionConfig)) *funcTcpSessionOption {
	return &funcTcpSessionOption{
		f: f,
	}
}

func WithContext(ctx context.Context, cancel context.CancelFunc) TcpSessionOption {
	return newFuncTcpSessionOption(func(c *tcpSessionConfig) {
		c.ctx = ctx
		c.cancel = cancel
	})
}
func WithLogger(logger logger.Logger) TcpSessionOption {
	return newFuncTcpSessionOption(func(c *tcpSessionConfig) {
		c.Log = logger
	})
}

func WithConfig(config config.Config) TcpSessionOption {
	return newFuncTcpSessionOption(func(c *tcpSessionConfig) {
		c.Config = config
	})
}

func WithOname(oname string) TcpSessionOption {
	return newFuncTcpSessionOption(func(c *tcpSessionConfig) {
		c.ObjectName = oname
	})
}

func WithAccessKey(accessKey string) TcpSessionOption {
	return newFuncTcpSessionOption(func(c *tcpSessionConfig) {
		c.License = accessKey
		c.AccessKey = accessKey

	})
}

func WithServers(servers []string) TcpSessionOption {
	return newFuncTcpSessionOption(func(c *tcpSessionConfig) {
		c.Servers = servers

	})
}

func WithWhatapTcpServer(info *wnet.WhatapTcpServerInfo) TcpSessionOption {
	return newFuncTcpSessionOption(func(c *tcpSessionConfig) {
		c.License = info.License
		c.AccessKey = info.License
		c.Servers = info.Hosts
		c.Pcode = info.Pcode
		c.Oid = info.Oid
	})
}
func WithUseQueue() TcpSessionOption {
	return newFuncTcpSessionOption(func(c *tcpSessionConfig) {
		c.UseQueue = true
	})
}

func WithConfigObserver(obj *config.ConfigObserver) TcpSessionOption {
	return newFuncTcpSessionOption(func(c *tcpSessionConfig) {
		c.ConfigObserver = obj
	})
}
