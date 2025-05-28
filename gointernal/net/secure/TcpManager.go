package secure

import (
	// "context"
	"sync"
	// "time"

	"github.com/whatap/golib/config"
	"github.com/whatap/golib/logger"
	// "github.com/whatap/golib/util/hmap"
	// "github.com/whatap/golib/util/queue"
)

type TcpManager struct {
}

var (
	tcpManager     *TcpManager = nil
	tcpManagerLock             = sync.Mutex{}
)

func GetInstanceTcpManager(opts ...TcpSessionOption) *TcpManager {
	tcpManagerLock.Lock()
	defer tcpManagerLock.Unlock()
	if tcpManager == nil {
		tcpManager = newTcpManager(opts...)
	}
	return tcpManager
}

func newTcpManager(opts ...TcpSessionOption) *TcpManager {
	p := new(TcpManager)

	o := &tcpSessionConfig{}
	for _, opt := range opts {
		opt.apply(o)
	}
	conf.Log = o.Log
	if conf.Log == nil {
		conf.Log = &logger.EmptyLogger{}
	}
	conf.License = o.License
	conf.Servers = o.Servers
	conf.Pcode = o.Pcode
	conf.Oid = o.Oid
	conf.ctx = o.ctx
	conf.cancel = o.cancel
	conf.Timeout = o.Timeout
	conf.ObjectName = o.ObjectName
	//p.lastTime = dateutil.SystemNow()
	// p.Log.Info("newOneWayTcpClient license=", p.License)
	if conf.ConfigObserver != nil {
		conf.ConfigObserver.Add("TcpManager", p)
	}

	return p
}

func (this *TcpManager) StartNet() {
	InitSender()
	InitReceiver()
	tcp := GetTcpSession()
	tcp.WaitForConnection()
}

func StartNet(opts ...TcpSessionOption) {
	InitSender()
	InitReceiver()
	GetInstanceTcpManager(opts...)
	tcp := GetTcpSession()
	tcp.WaitForConnection()

}

func (this *TcpManager) ApplyConfig(c config.Config) {
	o := &tcpSessionConfig{}

	o.TcpSoTimeout = c.GetInt("tcp_so_timeout", 120000)
	o.TcpSoSendTimeout = c.GetInt("tcp_so_send_timeout", 20000)
	o.TcpConnectionTimeout = c.GetInt("tcp_connection_timeout", 5000)

	o.NetWriteBufferSize = c.GetInt("net_write_buffer_size", 8*1024*1024)
	o.NetSendMaxBytes = c.GetInt("net_send_max_bytes", 5*1024*1024)
	o.NetSendBufferSize = c.GetInt("net_send_buffer_size", 1024)
	o.NetSendQueue1Size = c.GetInt("net_send_queue1_size", 512)
	o.NetSendQueue2Size = c.GetInt("net_send_queue2_size", 100000)

	o.NetWriteLockEnabled = c.GetBoolean("net_write_lock_enabled", true)

	o.CypherLevel = c.GetInt("cypher_level", 128)
	o.EncryptLevel = c.GetInt("encrypt_level", 2)

	o.QueueTcpEnabled = c.GetBoolean("queue_tcp_enabled", true)
	o.QueueLogEnabled = c.GetBoolean("queue_log_enabled", false)
	o.QueueTcpSenderThreadCount = c.GetInt("queue_tcp_sender_thread_count", 2)

	o.TimeSyncIntervalMs = c.GetLong("time_sync_interval_ms", 5000)

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
