package secure

import (
	"time"

	"github.com/whatap/golib/io"
	"github.com/whatap/golib/lang/pack"
	"github.com/whatap/golib/util/dateutil"
)

var start bool = false
var RecvBuffer chan pack.Pack

// InitReceiver initializes the receiver if it hasn't been started yet
func InitReceiver() {
	if !start {
		lock.Lock()
		defer lock.Unlock()

		if !start {
			start = true
			RecvBuffer = make(chan pack.Pack, 100)
			go run()
		}
	}
}

// run continuously receives data from the TCP session
func run() {
	secuMaster := GetSecurityMaster()
	secuMaster.WaitForInit()
	secuSession := GetSecuritySession()

	// Recovery function to prevent the loop from terminating
	recoverPanic := func() {
		if r := recover(); r != nil {
			conf.Log.Println("WA721", "Recovered from panic", r)
		}
	}

	for {
		func() {
			defer recoverPanic()

			session := GetTcpSession()
			for !session.IsOpen() {
				session = GetTcpSession()
				time.Sleep(1000 * time.Millisecond)
			}

			out := session.Read()
			if out.Code == NET_TIME_SYNC {
				in := io.NewDataInputX(out.Data)
				prevAgentTime := in.ReadLong()
				serverTime := in.ReadLong()
				now := dateutil.SystemNow()
				turnaroundTime := now - prevAgentTime

				// Only adjust time if the turnaround time is less than 500ms
				// The clock should always run slightly behind the server time
				// The clock will be delayed by the turnaround time
				// ServiceTime setting changed: SetServerTime recalculates the difference with server time and sets it as Delta
				if turnaroundTime < 500 {
					dateutil.SetServerTime(serverTime, 1)
				}
				return
			}

			if conf.CypherLevel > 0 {
				if out.TransferKey != secuSession.TRANSFER_KEY {
					return
				}

				switch GetSecureMask(out.Code) {
				case NET_SECURE_HIDE:
					if secuSession.Cypher != nil {
						out.Data = secuSession.Cypher.Hide(out.Data)
					}
				case NET_SECURE_CYPHER:
					if secuSession.Cypher != nil {
						out.Data = secuSession.Cypher.Decrypt(out.Data)
					}
				default:
					out.Data = nil
				}
			}

			if out.Data != nil && len(out.Data) > 0 {
				p := pack.ToPack(out.Data)
				RecvBuffer <- p
			}
		}()

		// Sleep to prevent high CPU usage
		time.Sleep(10 * time.Millisecond)
	}
}
