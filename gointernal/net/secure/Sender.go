package secure

import (
	"fmt"
	//"log"
	//"runtime/debug"
	"runtime"
	"sync"
	"time"

	"github.com/whatap/golib/io"
	"github.com/whatap/golib/lang/pack"

	"github.com/whatap/golib/util/dateutil"
	"github.com/whatap/golib/util/queue"
	"github.com/whatap/golib/util/stringutil"
)

type TcpSend struct {
	flag  byte
	pack  pack.Pack
	flush bool
}

var lock = sync.Mutex{}
var senderStart bool = false

// var senderLock = sync.Mutex{}
var TcpQueue *queue.RequestDoubleQueue

//var conf = config.GetConfig()

func Send(f byte, p pack.Pack, flush bool) {
	InitSender()
	if TcpQueue != nil {
		TcpQueue.Put1(TcpSend{f, p, flush})
	}
}

func SendProfile(f byte, p pack.Pack, flush bool) {
	InitSender()
	// DEBUG Queue
	if TcpQueue != nil {
		// profile 우선순위 낮게 처리
		TcpQueue.Put2(TcpSend{f, p, flush})
	}
}
func InitSender() {
	if TcpQueue == nil {
		lock.Lock()
		TcpQueue = queue.NewRequestDoubleQueue(int(conf.NetSendQueue1Size), int(conf.NetSendQueue2Size))
		if conf.QueueLogEnabled {
			conf.Log.Println("WA10900-02", "Tcp Sender Queue=", TcpQueue.GetCapacity1(), ",", TcpQueue.GetCapacity2(), ",thread_count=", conf.QueueTcpSenderThreadCount)
		}
		// 기본 1개
		for i := 0; i < int(conf.QueueTcpSenderThreadCount); i++ {
			//				conf.Log.Infoln("InitSender", "go Run")
			//				PrintMemUsage()
			go runSend()
		}

		defer lock.Unlock()
	}
}

func PrintMemUsage() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	// For info on each, see: https://golang.org/pkg/runtime/#MemStats
	conf.Log.Infof("MemStats", "Alloc = %v MiB", bToMb(m.Alloc))
	conf.Log.Infof("MemStats", "\tTotalAlloc = %v MiB", bToMb(m.TotalAlloc))
	conf.Log.Infof("MemStats", "\tSys = %v MiB", bToMb(m.Sys))
	conf.Log.Infof("MemStats", "\tNumGC = %v\n", m.NumGC)
}
func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}
func runSend() {
	cypher_level := conf.CypherLevel
	queue1Size := conf.NetSendQueue1Size
	queue2Size := conf.NetSendQueue2Size

	last_time_sync := int64(0)
	pack_len := 0

	// TODO 현재는 사용 안함.
	//var cnt int64 = 0

	for {
		// DEBUG goroutine 로그 출력
		//conf.Log.Println("Sender.runSend")

		//conf.Log.Println("[whatap_debug] send packet loop start")
		func() {
			// 20191107 sender를 여러개 돌리기 위해 senderLock 추가. 기존 lock 은 receiver, sender 모두 사용 중.
			lock.Lock()
			session := GetTcpSession()
			defer func() {
				fmt.Println("Sender.runSend defer")
				lock.Unlock()
				if x := recover(); x != nil {
					fmt.Println("Recover", x)
					conf.Log.Println("WA10901", " Recover", x) //, string(debug.Stack()))
					session.Close()
				}
			}()

			var p TcpSend

			if TcpQueue != nil {
				// Change queue size dynamically
				if queue1Size != conf.NetSendQueue1Size || queue2Size != conf.NetSendQueue2Size {
					TcpQueue.SetCapacity(int(conf.NetSendQueue1Size), int(conf.NetSendQueue2Size))
				}

				if conf.QueueLogEnabled {
					conf.Log.Println("WA10901-03", "Tcp queue len=", TcpQueue.Size1(), ",", TcpQueue.Size2())
				}

				if TcpQueue.Size1() == TcpQueue.GetCapacity1() {
					conf.Log.Println("WA10901-04", "Tcp Queue1 Full", TcpQueue.Size1())
				}
				if TcpQueue.Size2() == TcpQueue.GetCapacity2() {
					conf.Log.Println("WA10901-05", "Tcp Queue2 Full", TcpQueue.Size2())
				}
				v := TcpQueue.Get()
				if v == nil {
					conf.Log.Println("WA10901-06", "TcpQueue.Get is nil")
					return
				}
				p = v.(TcpSend)
			} else {
				conf.Log.Println("WA10901-07", "TcpQueue is nil")
			}

			//conf.Log.Println("isOpen")
			for session.IsOpen() == false {
				session = GetTcpSession()
				//fmt.Println("Sender.runSend waiting for session to open")
				time.Sleep(100 * time.Millisecond)
			}
			if cypher_level != conf.CypherLevel {
				cypher_level = conf.CypherLevel
				session.Close()
				return
			}

			secuTcp := GetSecuritySession()
			//now := dateutil.Now()
			now := dateutil.SystemNow()

			if now > last_time_sync+int64(conf.TimeSyncIntervalMs) {
				if conf.DebugTcpSendTimeSyncEnabled {
					conf.Log.Infoln("[DEBUG]", "NET_TIME_SYNC now=", now, ",last_time_sync=", last_time_sync, ",conf=", conf.TimeSyncIntervalMs)
				}
				last_time_sync = now
				session.Send(NET_TIME_SYNC, io.ToBytesLong(now), true)
			}

			if conf.CypherLevel == 0 {
				b := pack.ToBytesPack(p.pack)
				if conf.DebugTcpSendEnabled && stringutil.InArray(pack.GetPackTypeString((p.pack).GetPackType()), conf.DebugTcpSendPacks) {
					conf.Log.Infoln("[DEBUG]", "Send NET_NORMAL ", pack.GetPackTypeString((p.pack).GetPackType()), " flush=", p.flush, " size=", len(b)) //, p.pack)
				}
				if conf.NetFailoverRetrySendDataEnabled {
					session.RetryQueue.PutForce(&p)
				}
				session.Send(p.flag, b, p.flush)
				pack_len = len(b)
			} else {
				if conf.DebugTcpSendEnabled && stringutil.InArray(pack.GetPackTypeString((p.pack).GetPackType()), conf.DebugTcpSendPacks) {
					debugM := fmt.Sprintf("SendTest flag=%v//packType=%v,flush=%v\n", p.flag, p.pack.GetPackType(), p.flush)
					conf.Log.Infoln("[DEBUG]", debugM)
				}
				switch GetSecureMask(p.flag) {
				case NET_SECURE_HIDE:
					if secuTcp.Cypher != nil {
						b := pack.ToBytesPack(p.pack)
						b = secuTcp.Cypher.Hide(b)
						if conf.DebugTcpSendEnabled && stringutil.InArray(pack.GetPackTypeString((p.pack).GetPackType()), conf.DebugTcpSendPacks) {
							conf.Log.Infoln("[DEBUG]", "Send NET_SECURE_HIDE ", pack.GetPackTypeString((p.pack).GetPackType()), " flush=", p.flush, " size=", len(b)) //, p.pack)
						}
						if conf.NetFailoverRetrySendDataEnabled {
							session.RetryQueue.PutForce(&p)
						}
						if session.Send(p.flag, b, p.flush) == false {
							fmt.Println("[whatap_debug] send secure failed")
						}
						pack_len = len(b)
					} else {
						// send default
						b := pack.ToBytesPack(p.pack)
						if conf.DebugTcpSendEnabled && stringutil.InArray(pack.GetPackTypeString((p.pack).GetPackType()), conf.DebugTcpSendPacks) {
							conf.Log.Infoln("[DEBUG]", "Send NET_SECURE_HIDE Default ", pack.GetPackTypeString((p.pack).GetPackType()), " flush=", p.flush, " size=", len(b)) //, p.pack)
						}
						if conf.NetFailoverRetrySendDataEnabled {
							session.RetryQueue.PutForce(&p)
						}
						session.Send(p.flag, b, p.flush)
						pack_len = len(b)
					}
				case NET_SECURE_CYPHER:
					if secuTcp.Cypher != nil {
						b := pack.ToBytesPackECB(p.pack, int(conf.CypherLevel/8)) // 16bytes배수로
						b = secuTcp.Cypher.Encrypt(b)
						if conf.DebugTcpSendEnabled && stringutil.InArray(pack.GetPackTypeString((p.pack).GetPackType()), conf.DebugTcpSendPacks) {
							conf.Log.Infoln("[DEBUG]", "Send NET_SECURE_CYPHER ", pack.GetPackTypeString((p.pack).GetPackType()), " flush=", p.flush, " size=", len(b)) //, p.pack)
						}
						if conf.NetFailoverRetrySendDataEnabled {
							session.RetryQueue.PutForce(&p)
						}
						if session.Send(p.flag, b, p.flush) == false {
							//fmt.Println("[whatap_debug] send secure failed")
						}
						pack_len = len(b)
					} else {
						// send default
						b := pack.ToBytesPack(p.pack)
						if conf.DebugTcpSendEnabled && stringutil.InArray(pack.GetPackTypeString((p.pack).GetPackType()), conf.DebugTcpSendPacks) {
							conf.Log.Infoln("[DEBUG]", "Send NET_SECURE_CYPHER Default ", pack.GetPackTypeString((p.pack).GetPackType()), " flush=", p.flush, " size=", len(b)) //, p.pack)
						}
						if conf.NetFailoverRetrySendDataEnabled {
							session.RetryQueue.PutForce(&p)
						}
						session.Send(p.flag, b, p.flush)
						pack_len = len(b)
					}
				default:
					b := pack.ToBytesPack(p.pack)
					if conf.DebugTcpSendEnabled && stringutil.InArray(pack.GetPackTypeString((p.pack).GetPackType()), conf.DebugTcpSendPacks) {
						conf.Log.Infoln("[DEBUG]", "Send Default ", pack.GetPackTypeString((p.pack).GetPackType()), " flush=", p.flush, " size=", len(b)) //, p.pack)
					}
					if conf.NetFailoverRetrySendDataEnabled {
						session.RetryQueue.PutForce(&p)
					}
					if session.Send(p.flag, b, p.flush) == false {
						//fmt.Println("[whatap_debug] send failed")
					}
					pack_len = len(b)
				}
			}
			if int32(pack_len) > conf.NetSendMaxBytes {
				p := pack.NewEventPack()
				p.Level = pack.FATAL
				p.Title = "NEW_OVERFLOW"
				p.Message = "Too big data: " + pack.GetPackTypeString(p.GetPackType())
				conf.Log.Println("WA10902 ", p.Title, ",", p.Message)
				Send(NET_SECURE_CYPHER, p, true)
				return
			}
		}()
		//conf.Log.Println("[whatap_debug] send packet loop complete")
	}
}
