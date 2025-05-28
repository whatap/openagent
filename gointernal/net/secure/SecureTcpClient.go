package secure

import (
	// "context"
	"sync"
	// "time"
	"fmt"

	"github.com/whatap/golib/config"
	"github.com/whatap/golib/lang/pack"
	"github.com/whatap/golib/logger"
	wnet "github.com/whatap/golib/net"
	// "github.com/whatap/golib/util/hmap"
	// "github.com/whatap/golib/util/queue"
)

type SecureTcpClient struct {
}

var (
	secureTcpClient     *SecureTcpClient = nil
	secureTcpClientLock                  = sync.Mutex{}
)

func GetSecureTcpClient(opts ...TcpSessionOption) *SecureTcpClient {
	secureTcpClientLock.Lock()
	defer secureTcpClientLock.Unlock()
	if secureTcpClient == nil {
		secureTcpClient = newSecureTcpClient(opts...)
	}
	return secureTcpClient
}

func newSecureTcpClient(opts ...TcpSessionOption) *SecureTcpClient {
	p := new(SecureTcpClient)

	// conf is tcpSessionConfig
	for _, opt := range opts {
		opt.apply(conf)
	}
	if conf.Log == nil {
		conf.Log = &logger.EmptyLogger{}
	}
	if conf.ConfigObserver != nil {
		conf.ConfigObserver.Add("SecureTcpClient", p)
	}
	return p
}

func (this *SecureTcpClient) StartNet() {
	InitSender()
	InitReceiver()
	tcp := GetTcpSession()
	tcp.WaitForConnection()
}

func (this *SecureTcpClient) Connect() (err error) {
	defer func() {
		if r := recover(); r != nil {
			conf.Log.Println(">>>>", "Recover ", r)
			err = fmt.Errorf("Error Recover %v", r)
		}
	}()
	InitSender()
	InitReceiver()
	tcp := GetTcpSession()
	tcp.WaitForConnection()
	return err
}
func (this *SecureTcpClient) Send(p pack.Pack, opts ...wnet.TcpClientOption) error {
	o := &wnet.TcpClientConfig{}
	for _, opt := range opts {
		opt.Apply(o)
	}

	// TODO
	//Send(NET_SECURE_HIDE, p, false)
	if o.Priority {
		Send(o.SecureFlag, p, false)
	} else {
		SendProfile(o.SecureFlag, p, false)
	}

	return nil
}
func (this *SecureTcpClient) SendFlush(p pack.Pack, flush bool, opts ...wnet.TcpClientOption) error {
	o := &wnet.TcpClientConfig{}
	for _, opt := range opts {
		opt.Apply(o)
	}
	// TODO
	//Send(NET_SECURE_HIDE, p, false)
	if o.Priority {
		Send(o.SecureFlag, p, false)
	} else {
		SendProfile(o.SecureFlag, p, false)
	}
	return nil
}
func (this *SecureTcpClient) Close() error {
	GetTcpSession().Close()
	return nil
}

func (this *SecureTcpClient) ApplyConfig(c config.Config) {
	o := &tcpSessionConfig{}

	o.TcpSoTimeout = c.GetInt("tcp_so_timeout", 120000)
	o.TcpSoSendTimeout = c.GetInt("tcp_so_send_timeout", 20000)
	o.TcpConnectionTimeout = c.GetInt("tcp_connection_timeout", 5000)

	o.NetWriteBufferSize = c.GetInt("net_write_buffer_size", 8*1024*1024)
	o.NetSendMaxBytes = c.GetInt("net_send_max_bytes", 5*1024*1024)
	o.NetSendBufferSize = c.GetInt("net_send_buffer_size", 1024)
	o.NetSendQueue1Size = c.GetInt("net_send_queue1_size", 512)
	o.NetSendQueue2Size = c.GetInt("net_send_queue2_size", 1024)

	o.NetWriteLockEnabled = c.GetBoolean("net_write_lock_enabled", true)

	o.CypherLevel = c.GetInt("cypher_level", 128)
	o.EncryptLevel = c.GetInt("encrypt_level", 2)

	o.QueueTcpEnabled = c.GetBoolean("queue_tcp_enabled", true)
	o.QueueLogEnabled = c.GetBoolean("queue_log_enabled", false)
	o.QueueTcpSenderThreadCount = c.GetInt("queue_tcp_sender_thread_count", 2)

	o.TimeSyncIntervalMs = c.GetLong("time_sync_interval_ms", 30000)

	o.DebugTcpSendEnabled = c.GetBoolean("debug_tcpsend_enabled", false)
	o.DebugTcpSendPacks = c.GetStringArray("debug_tcpsend_packs", "CounterPack1", ",")
	o.DebugTcpFailoverEnabled = c.GetBoolean("debug_tcp_failover_enabled", false)

	o.DebugTcpReadEnabled = c.GetBoolean("debug_tcpread_enabled", false)
	o.DebugTcpSendTimeSyncEnabled = c.GetBoolean("debug_tcpsend_timesync_enabled", false)
	o.NetFailoverRetrySendDataEnabled = c.GetBoolean("net_failover_retry_send_data_enabled", false)

	o.MeterSelfEnabled = c.GetBoolean("meter_self_enabled", true)

	if o.NetSendQueue1Size != conf.NetSendQueue1Size || o.NetSendQueue2Size != conf.NetSendQueue2Size {
		TcpQueue.SetCapacity(int(conf.NetSendQueue1Size), int(conf.NetSendQueue2Size))
	}
	// o.QueueTcpSenderThreadCount = getInt("queue_tcp_sender_thread_count", 2)
	conf = o

	// 라이센스 변경사항 확인
	GetSecurityMaster().Run()
}
