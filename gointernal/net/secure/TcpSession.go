package secure

import (
	"fmt"
	"net"
	"sync"

	//"syscall"
	"bufio"
	"runtime/debug"
	"time"

	//"github.com/whatap/golib/util/conf.Log"
	"github.com/whatap/golib/io"
	"github.com/whatap/golib/lang/pack"
	"github.com/whatap/golib/util/dateutil"
	"github.com/whatap/golib/util/queue"
	"github.com/whatap/golib/util/stringutil"
	// "gitlab.whatap.io/go/agent/agent/counter/meter"
)

const (
	READ_MAX = 8 * 1024 * 1024
)

type TcpSession struct {
	client            net.Conn
	wr                *bufio.Writer
	in                *io.DataInputX
	dest              int
	LastConnectedTime int64

	RetryQueue *queue.RequestQueue
}

type TcpReturn struct {
	Code        byte
	Data        []byte
	TransferKey int32
}

var sessionLock = sync.Mutex{}
var sendLock = sync.Mutex{}
var session *TcpSession

func GetTcpSession() *TcpSession {
	sessionLock.Lock()
	defer sessionLock.Unlock()
	if session != nil {
		return session
	}
	session = new(TcpSession)
	session.RetryQueue = queue.NewRequestQueue(256)

	go func() {
		for {
			for session.open() == false {
				time.Sleep(3000 * time.Millisecond)
			}

			//conf.Log.Println("isOpen")
			for session.IsOpen() {
				time.Sleep(5000 * time.Millisecond)
			}
		}

	}()

	return session
}

func (this *TcpSession) open() (ret bool) {
	sessionLock.Lock()
	defer func() {
		sessionLock.Unlock()
		if x := recover(); x != nil {
			conf.Log.Println("WA172", x, string(debug.Stack()))
			ret = false
			this.Close()
		}
	}()

	if this.IsOpen() {
		return true
	}

	if GetSecurityMaster().PCODE == 0 {
		this.Close()
		return false
	}

	// DEBUG TEST
	// conf := config.GetConfig()
	// conf.ConfDebugTest.DebugCloseTcpFunc = this.Close

	//hosts := conf.WhatapHost
	//if hosts == nil || len(hosts) == 0 {
	if conf.Servers == nil || len(conf.Servers) == 0 {
		this.Close()
		return false
	}

	this.dest += 1
	//if this.dest >= len(hosts) {
	if this.dest >= len(conf.Servers) {
		this.dest = 0
	}

	//port := conf.WhatapPort
	conf.Log.Debug(conf.Servers[this.dest])
	client, err := net.DialTimeout("tcp", conf.Servers[this.dest], time.Duration(conf.TcpConnectionTimeout)*time.Millisecond)
	if err != nil {
		conf.Log.Println("WA173", "Connection error. (invalid whatap.server.host key error.)", err)
		if client != nil {
			client.Close()
		}
		this.Close()
		return false
	}
	secure := GetSecurityMaster()
	secure.DecideAgentOnameOid(stringutil.Tokenizer(client.LocalAddr().String(), ":")[0])
	conf.Log.Infoln(">>>>", "oname=", secure.ONAME, ",oid=", secure.OID)
	client.SetDeadline(time.Now().Add(time.Duration(conf.TcpSoTimeout) * time.Millisecond))
	conf.Log.Infoln(">>>>", "Write KeyReset")
	client.Write(this.keyReset())
	this.in = io.NewDataInputNet(client)
	data := this.readKeyReset(this.in)
	conf.Log.Infoln(">>>>", "KeyReset")
	UpdateNetCypherKey(data)
	conf.Log.Infoln(">>>>", "UpdateNetCypherKey")
	this.wr = bufio.NewWriterSize(client, int(conf.NetWriteBufferSize))

	conf.Log.Infoln("WA171", "PCODE=", secure.PCODE, " OID=", secure.OID, " ONAME=", secure.ONAME)
	conf.Log.Infoln("WA174", "Net TCP: Connect to ", client.RemoteAddr().String())
	this.LastConnectedTime = dateutil.SystemNow()

	if conf.NetFailoverRetrySendDataEnabled {
		// ignore result
		this.SendFailover()
		//		if !this.SendFailover() {
		//			if client != nil {
		//				client.Close()
		//			}
		//			this.Close()
		//			return false
		//		}
	}
	this.RetryQueue.Clear()

	this.client = client

	return true
}
func (this *TcpSession) IsOpen() bool {
	//conf.Log.Printf("Client %p", this.client)
	return this.client != nil
}
func (this *TcpSession) readKeyReset(in *io.DataInputX) []byte {
	conf.Log.Println(">>>>", "readKeyReset start")
	defer func() {
		if r := recover(); r != nil {
			panic(fmt.Sprintln("invalid license key error. ", r))
		}
	}()
	_ = in.ReadByte()
	_ = in.ReadByte()
	pcode := in.ReadLong()
	oid := in.ReadInt()
	_ = in.ReadInt()
	data := in.ReadIntBytesLimit(1024)
	secu := GetSecurityMaster()

	conf.Log.Println(">>>>> readKeyReset", "pcod=", pcode, ", oid=", oid)
	if pcode != secu.PCODE || oid != secu.OID {
		return []byte{}
	} else {
		return data
	}
}
func (this *TcpSession) keyReset() []byte {
	defer func() {
		err := recover()
		if err != nil {
			conf.Log.Println("WA175", "Recover ", err)
		}
	}()
	secu := GetSecurityMaster()
	secu.WaitForInit()
	dout := io.NewDataOutputX()

	conf.Log.Println(">>>>", "keyReset hello", "oname=", secu.ONAME, ",ip=", secu.IP)
	msg := dout.WriteText("hello").WriteText(secu.ONAME).WriteInt(secu.IP).ToByteArray()
	if conf.CypherLevel > 0 {
		msg = secu.Cypher.Encrypt(msg)
	}
	dout = io.NewDataOutputX()
	dout.WriteByte(NETSRC_AGENT_JAVA_EMBED)

	var trkey int32 = 0
	if conf.CypherLevel == 128 {
		dout.WriteByte(byte(NET_KEY_RESET))
	} else {
		dout.WriteByte(byte(NET_KEY_EXTENSION))

		if conf.CypherLevel == 0 {
			trkey = 0
		} else {
			b0 := byte(1)
			b1 := byte(conf.CypherLevel / 8)
			trkey = io.ToInt([]byte{byte(b0), byte(b1), byte(0), byte(0)}, 0)
		}
	}
	dout.WriteLong(secu.PCODE)
	dout.WriteInt(secu.OID)
	dout.WriteInt(trkey)
	dout.WriteIntBytes(msg)

	conf.Log.Infoln(">>>>", "keyReset license=", conf.AccessKey, ", CypherLevel=", conf.CypherLevel, ",pcode=", secu.PCODE, ", oid=", secu.OID, ", trkey=", trkey, ", msg=", msg)

	return dout.ToByteArray()
}

func (this *TcpSession) Send(code byte, b []byte, flush bool) (ret bool) {
	defer func() {
		if x := recover(); x != nil {
			ret = false
			conf.Log.Println("WA176", " Send Recover ", x)
			this.Close()
		}
	}()

	secu := GetSecurityMaster()
	secuSession := GetSecuritySession()
	out := io.NewDataOutputX()
	out.WriteByte(NETSRC_AGENT_JAVA_EMBED)
	out.WriteByte(code)
	out.WriteLong(secu.PCODE)
	out.WriteInt(secu.OID)
	out.WriteInt(secuSession.TRANSFER_KEY)
	out.WriteIntBytes(b)
	sendbuf := out.ToByteArray()

	if this.client == nil {
		conf.Log.Println("WA176-01", " this.client is nil ")
		return false
	}

	// set SetWriteDeadline Write i/o timeout 처리 , Write 전에 반복해서 Deadline 설정
	err := session.client.SetWriteDeadline(time.Now().Add(time.Duration(conf.TcpSoSendTimeout) * time.Millisecond))
	if err != nil {
		conf.Log.Println("WA177", " SetWriteDeadline failed:", err)
		return false
	}
	if conf.NetWriteLockEnabled {
		if _, err := this.WriteLock(sendbuf); err != nil {
			conf.Log.Println("WA179-0201", " Write Lock Error", err, ",stack=", string(debug.Stack()))
			this.Close()
			return false
		}
	} else {
		if _, err := this.Write(sendbuf); err != nil {
			conf.Log.Println("WA179-02", " Write Error", err, ",stack=", string(debug.Stack()))
			this.Close()
			return false
		}
	}
	if flush {
		if n, err := this.Flush(); err != nil {
			conf.Log.Println("WA179-03", " Flush Error", err, ",stack=", string(debug.Stack()))
			this.Close()
			return false
		} else {
			// DEBUG Meter Self
			// if conf.MeterSelfEnabled {
			// 	meter.GetInstanceMeterSelf().AddMeterSelfPacket(int64(n))
			// }
			// clear temp when bufio flush
			if conf.DebugTcpSendEnabled || conf.DebugTcpFailoverEnabled {
				conf.Log.Infoln("WA174-D-03", "flush=", n, ", retry queue reset sz=", this.RetryQueue.Size())
			}
			this.RetryQueue.Clear()
		}
	}
	return true
}
func (this *TcpSession) SendFailover() bool {
	if conf.DebugTcpFailoverEnabled {
		conf.Log.Infoln("WA174-D-01", "Open retry queue sz=", this.RetryQueue.Size())
	}
	if this.RetryQueue.Size() > 0 {
		sz := this.RetryQueue.Size()
		for i := 0; i < sz; i++ {
			v := this.RetryQueue.GetNoWait()
			if v == nil {
				continue
			}
			temp := v.(*TcpSend)
			secu := GetSecurityMaster()
			secuSession := GetSecuritySession()
			if n, b, err := this.getEncryptData(temp); err == nil {
				out := io.NewDataOutputX()
				out.WriteByte(NETSRC_AGENT_JAVA_EMBED)
				out.WriteByte(temp.flag)
				out.WriteLong(secu.PCODE)
				out.WriteInt(secu.OID)
				out.WriteInt(secuSession.TRANSFER_KEY)
				out.WriteIntBytes(b)
				sendbuf := out.ToByteArray()
				if conf.NetWriteLockEnabled {
					if _, err := this.WriteLock(sendbuf); err != nil {
						conf.Log.Println("WA174-0301", "Error Write Lock Retry ", "t=", pack.GetPackTypeString(temp.pack.GetPackType()), ", n=", n, ",", err)
						return false
					}
				} else {
					if _, err := this.Write(sendbuf); err != nil {
						conf.Log.Println("WA174-03", "Error Write Retry ", "t=", pack.GetPackTypeString(temp.pack.GetPackType()), ", n=", n, ",", err)
						return false
					}
				}
			}
		}
		if n, err := this.Flush(); err != nil {
			conf.Log.Println("WA174-04", "Error Flush Retry ", ",", err)
			return false
		} else {
			// DEBUG Meter Self
			// if conf.MeterSelfEnabled {
			// 	meter.GetInstanceMeterSelf().AddMeterSelfPacket(int64(n))
			// }
			// clear temp when bufio flush
			if conf.DebugTcpFailoverEnabled {
				conf.Log.Infoln("WA174-D-04", "Open retry counter ", "flush=", n)
			}
		}
	}

	return true
}

func (this *TcpSession) Write(sendbuf []byte) (int, error) {
	nbyteleft := len(sendbuf)
	// 다 보내지지 않았을 경우 추가 전송을 위핸 변수 설정.
	pos := 0

	for 0 < nbyteleft {
		nbytethistime, err := this.wr.Write(sendbuf[pos : pos+nbyteleft])
		if err != nil {
			return pos, err
		}

		// DEBUG 로그
		if nbyteleft > nbytethistime {
			conf.Log.Printf("WA179", "available=%d, send=%d, remine=%d", nbyteleft, nbytethistime, (nbyteleft - nbytethistime))
		}

		nbyteleft -= nbytethistime
		pos += nbytethistime
	}
	return pos, nil
}

func (this *TcpSession) WriteLock(sendbuf []byte) (int, error) {
	sendLock.Lock()
	defer sendLock.Unlock()
	return this.Write(sendbuf)
}

func (this *TcpSession) Flush() (n int, err error) {
	n = this.wr.Buffered()
	if err = this.wr.Flush(); err != nil {
		return 0, err
	}
	return n, nil
}
func (this *TcpSession) Close() {
	conf.Log.Infoln("WA181", " Close TCP connection")
	if this.client != nil {
		defer func() {
			if r := recover(); r != nil {
				conf.Log.Println("WA181-01", " Close Recover", string(debug.Stack()))
			}
			this.client = nil
		}()

		this.client.Close()
	}
	this.client = nil
	this.LastConnectedTime = 0
}

func (this *TcpSession) WaitForConnection() {
	//conf.Log.Println("isOpen")
	for this.IsOpen() == false {
		time.Sleep(1000 * time.Millisecond)
	}
}

var empty *TcpReturn = new(TcpReturn)

func (this *TcpSession) Read() (ret *TcpReturn) {
	// DataInputX 에서 Panic 발생 (EOF), 기타 오류 시 커넥션 종료
	// return empty 처리
	defer func() {
		if x := recover(); x != nil {
			ret = empty
			conf.Log.Println("WA183 Read Recover ", x, "\n", string(debug.Stack()))
			this.Close()
		}
	}()

	//conf.Log.Println("isOpen")
	if this.IsOpen() == false {
		return empty
	}

	// set SetReadDeadline Read i/o timeout 처리 , Read 전에 반복해서 Deadline 설정
	err := session.client.SetReadDeadline(time.Now().Add(time.Duration(conf.TcpSoTimeout) * time.Millisecond))
	// DEBUG goroutine 로그
	//conf.Log.Println("SetReadDeadline =", time.Now(), ",deadline=", time.Now().Add(time.Duration(conf.TcpSoTimeout)*time.Millisecond))
	if err != nil {
		conf.Log.Println("WA182 SetReadDeadline failed:", err)
		//return
	}

	tt := this.in.ReadByte()
	code := this.in.ReadByte()
	pcode := this.in.ReadLong()
	oid := this.in.ReadInt()
	transfer_key := this.in.ReadInt()
	if conf.DebugTcpReadEnabled {
		conf.Log.Infoln("WA182-02", "Tcp Receive ", tt, ", ", code, ", ", pcode, ", ", oid, ", ", transfer_key)
	}

	data := this.in.ReadIntBytesLimit(READ_MAX)
	secu := GetSecurityMaster()
	if pcode != secu.PCODE || oid != secu.OID {
		return empty
	}

	return &TcpReturn{Code: code, Data: data, TransferKey: transfer_key}

}

func (this *TcpSession) getEncryptData(p *TcpSend) (n int, b []byte, err error) {
	secuTcp := GetSecuritySession()
	if conf.CypherLevel == 0 {
		b = pack.ToBytesPack(p.pack)
		n = len(b)
	} else {
		switch GetSecureMask(p.flag) {
		case NET_SECURE_HIDE:
			if secuTcp.Cypher != nil {
				b = pack.ToBytesPack(p.pack)
				b = secuTcp.Cypher.Hide(b)
				n = len(b)
			} else {
				// send default
				b = pack.ToBytesPack(p.pack)
				n = len(b)
			}
		case NET_SECURE_CYPHER:
			if secuTcp.Cypher != nil {
				b = pack.ToBytesPackECB(p.pack, int(conf.CypherLevel/8)) // 16bytes배수로
				b = secuTcp.Cypher.Encrypt(b)
				n = len(b)
			} else {
				// send default
				b = pack.ToBytesPack(p.pack)
				n = len(b)
			}
		default:
			b := pack.ToBytesPack(p.pack)
			n = len(b)
		}
	}
	if n > int(conf.NetSendMaxBytes) {
		p := pack.NewEventPack()
		p.Level = pack.FATAL
		p.Title = "NEW_OVERFLOW"
		p.Message = "Too big data: " + pack.GetPackTypeString(p.GetPackType())
		conf.Log.Println("WA185", p.Title, ",", p.Message)
		err = fmt.Errorf("%s", p.Message)
		Send(NET_SECURE_CYPHER, p, true)
		return n, b, err
	} else {
		return n, b, nil
	}
}
