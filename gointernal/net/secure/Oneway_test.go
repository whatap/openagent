package secure

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	stdIO "io"
	"net"
	"testing"
	"time"

	"github.com/whatap/golib/lang/pack"
	"github.com/whatap/golib/lang/value"
	"github.com/whatap/golib/logger"
	"github.com/whatap/golib/util/queue"
)

func onewayTestQueue(t *testing.T, capacity int) *queue.RequestDoubleQueue {
	t.Helper()
	oldConf, oldQueue := conf, TcpQueue
	oldSession, oldMaster, oldSecurity := session, master, secSession
	c := *conf
	c.Log = &logger.EmptyLogger{}
	c.TcpSoSendTimeout = 2000
	c.NetSendMaxBytes = 4096
	c.NetFailoverRetrySendDataEnabled = false
	conf = &c
	TcpQueue = queue.NewRequestDoubleQueue(capacity, 2)
	t.Cleanup(func() {
		if session != oldSession || master != oldMaster || secSession != oldSecurity {
			t.Error("oneway operation initialized a runtime singleton")
		}
		conf, TcpQueue = oldConf, oldQueue
	})
	return TcpQueue
}

func onewayTestPack(pcode int64) *pack.ParamPack {
	p := pack.NewParamPack()
	p.Pcode = pcode
	p.Oid = 23
	p.Time = 123456789
	// Reuse an existing command only as a codec fixture; allocate no business ID.
	p.Id = KEEPALIVE
	p.PutString("instanceId", "i-synthetic")
	p.PutLong("stateCode", 16)
	p.PutString("stateName", "running")
	return p
}

// The receiver parses the go-apm envelope independently of the sender encoder.
func onewayPipeFrame(t *testing.T, send func(*TcpSession) bool) []byte {
	t.Helper()
	return onewayPipeFrames(t, 1, send)[0]
}

func onewayPipeFrames(t *testing.T, count int, send func(*TcpSession) bool) [][]byte {
	t.Helper()
	client, receiver := net.Pipe()
	t.Cleanup(func() { client.Close(); receiver.Close() })
	if err := receiver.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	s := &TcpSession{client: client, wr: bufio.NewWriterSize(client, 64), RetryQueue: queue.NewRequestQueue(4)}
	done := make(chan bool, 1)
	go func() { done <- send(s) }()
	frames := make([][]byte, 0, count)
	for i := 0; i < count; i++ {
		header := make([]byte, 2+8+8+4)
		_, err := stdIO.ReadFull(receiver, header)
		if err != nil {
			receiver.Close()
			<-done
			t.Fatalf("receiver could not read frame header: %v", err)
		}
		n := binary.BigEndian.Uint32(header[18:22])
		if n > 4096 {
			receiver.Close()
			<-done
			t.Fatalf("unexpected payload size: %d", n)
		}
		body := make([]byte, n)
		if _, err := stdIO.ReadFull(receiver, body); err != nil {
			receiver.Close()
			<-done
			t.Fatalf("receiver could not read pack: %v", err)
		}
		frames = append(frames, append(header, body...))
	}
	if !<-done {
		t.Fatal("session reported a send failure")
	}
	return frames
}

func assertOnewayFrame(t *testing.T, frame []byte, pcode, licenseHash int64, want *pack.ParamPack) {
	t.Helper()
	if frame[0] != 10 || frame[1] != 0 {
		t.Fatalf("go-apm oneway source/version = %d/%d, want 10/0", frame[0], frame[1])
	}
	if int64(binary.BigEndian.Uint64(frame[2:10])) != pcode {
		t.Error("oneway envelope changed destination project")
	}
	if int64(binary.BigEndian.Uint64(frame[10:18])) != licenseHash {
		t.Error("oneway envelope changed destination identity")
	}
	got, ok := pack.ToPack(frame[22:]).(*pack.ParamPack)
	if !ok {
		t.Fatalf("received %T, want ParamPack", got)
	}
	if got.Pcode != pcode || got.Oid != want.Oid || got.Time != want.Time || got.Id != want.Id || got.Request != 0 || got.Response != 0 {
		t.Error("ParamPack header was not preserved")
	}
	if got.GetString("instanceId") != want.GetString("instanceId") || got.GetLong("stateCode") != want.GetLong("stateCode") || got.GetString("stateName") != want.GetString("stateName") {
		t.Error("ParamPack values were not preserved")
	}
	if !bytes.Equal(frame[22:], pack.ToBytesPack(want)) {
		t.Error("payload differs from raw pack codec (padding/encryption/mutation)")
	}
}

func TestOnewayQueueToPipeReceiver(t *testing.T) {
	q := onewayTestQueue(t, 2)
	p := onewayTestPack(101)
	if err := SendOneway(101, -42, p); err != nil {
		t.Fatal(err)
	}
	v := q.GetNoWait()
	item, ok := v.(TcpSend)
	if !ok {
		t.Fatalf("queue entry is %T, want TcpSend", v)
	}
	frame := onewayPipeFrame(t, func(s *TcpSession) bool { return s.sendQueuedOneway(&item) })
	assertOnewayFrame(t, frame, 101, -42, p)
}

func TestOnewayRejectsInvalidEnvelope(t *testing.T) {
	tests := []struct {
		name   string
		pcode  int64
		hash   int64
		change func(*pack.ParamPack) *pack.ParamPack
	}{
		{name: "zero project", pcode: 0, hash: -42},
		{name: "negative project", pcode: -1, hash: -42},
		{name: "missing identity", pcode: 101, hash: 0},
		{name: "nil pack", pcode: 101, hash: -42, change: func(*pack.ParamPack) *pack.ParamPack { return nil }},
		{name: "missing pack project", pcode: 101, hash: -42, change: func(p *pack.ParamPack) *pack.ParamPack { p.Pcode = 0; return p }},
		{name: "mismatched pack project", pcode: 101, hash: -42, change: func(p *pack.ParamPack) *pack.ParamPack { p.Pcode = 202; return p }},
		{name: "zero command", pcode: 101, hash: -42, change: func(p *pack.ParamPack) *pack.ParamPack { p.Id = 0; return p }},
		{name: "negative command", pcode: 101, hash: -42, change: func(p *pack.ParamPack) *pack.ParamPack { p.Id = -1; return p }},
		{name: "request", pcode: 101, hash: -42, change: func(p *pack.ParamPack) *pack.ParamPack { p.Request = 1; return p }},
		{name: "negative request", pcode: 101, hash: -42, change: func(p *pack.ParamPack) *pack.ParamPack { p.Request = -1; return p }},
		{name: "response", pcode: 101, hash: -42, change: func(p *pack.ParamPack) *pack.ParamPack { p.Response = 1; return p }},
		{name: "negative response", pcode: 101, hash: -42, change: func(p *pack.ParamPack) *pack.ParamPack { p.Response = -1; return p }},
		{name: "uninitialized pack", pcode: 101, hash: -42, change: func(p *pack.ParamPack) *pack.ParamPack {
			return &pack.ParamPack{AbstractPack: p.AbstractPack, Id: KEEPALIVE}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := onewayTestQueue(t, 2)
			p := onewayTestPack(tt.pcode)
			if tt.change != nil {
				p = tt.change(p)
			}
			defer func() {
				if recover() != nil {
					t.Error("invalid envelope panicked instead of returning an error")
				}
				if q.Size() != 0 {
					t.Error("invalid envelope was admitted to the queue")
				}
			}()
			if err := SendOneway(tt.pcode, tt.hash, p); err == nil {
				t.Error("invalid envelope was accepted")
			}
		})
	}
}

func TestOnewayQueueAdmissionIsBounded(t *testing.T) {
	for _, capacity := range []int{0, -1} {
		t.Run(fmt.Sprintf("unbounded_%d", capacity), func(t *testing.T) {
			q := onewayTestQueue(t, capacity)
			if err := SendOneway(101, -42, onewayTestPack(101)); err == nil || q.Size() != 0 {
				t.Error("oneway accepted an unbounded queue")
			}
		})
	}
	t.Run("full", func(t *testing.T) {
		q := onewayTestQueue(t, 1)
		first := onewayTestPack(101)
		if err := SendOneway(101, -42, first); err != nil {
			t.Fatal(err)
		}
		if err := SendOneway(202, 43, onewayTestPack(202)); err == nil {
			t.Error("full queue was accepted")
		}
		if q.Size1() != 1 || q.Size2() != 0 {
			t.Fatal("full queue changed size or overflowed to the profile queue")
		}
		item := q.GetNoWait().(TcpSend)
		frame := onewayPipeFrame(t, func(s *TcpSession) bool { return s.sendQueuedOneway(&item) })
		assertOnewayFrame(t, frame, 101, -42, first)
	})
	t.Run("uninitialized", func(t *testing.T) {
		onewayTestQueue(t, 2)
		TcpQueue = nil
		if err := SendOneway(101, -42, onewayTestPack(101)); err == nil {
			t.Error("uninitialized queue was accepted")
		}
		if TcpQueue != nil {
			t.Error("oneway booted the sender rather than using the existing connection")
		}
	})
}

func TestOnewayQueueAdmissionRespectsPayloadLimit(t *testing.T) {
	p := onewayTestPack(101)
	size := len(pack.ToBytesPack(p))
	for _, limit := range []int{size, size - 1, 0, -1} {
		t.Run(fmt.Sprintf("limit_%d", limit), func(t *testing.T) {
			q := onewayTestQueue(t, 2)
			conf.NetSendMaxBytes = int32(limit)
			err := SendOneway(101, -42, p)
			if limit == size {
				if err != nil || q.Size() != 1 {
					t.Error("exact-size payload was not accepted")
				}
			} else if err == nil || q.Size() != 0 {
				t.Error("oversized payload or disabled limit was accepted")
			}
		})
	}
}

func TestOnewayFailedWriteRetainsRetry(t *testing.T) {
	q := onewayTestQueue(t, 2)
	conf.NetFailoverRetrySendDataEnabled = true
	p := onewayTestPack(101)
	if err := SendOneway(101, -42, p); err != nil {
		t.Fatal(err)
	}
	item := q.GetNoWait().(TcpSend)
	client, receiver := net.Pipe()
	defer client.Close()
	receiver.Close()
	retryQueue := queue.NewRequestQueue(4)
	s := &TcpSession{client: client, wr: bufio.NewWriter(client), RetryQueue: retryQueue}
	if s.sendQueuedOneway(&item) {
		t.Fatal("write to a closed receiver was reported as sent")
	}
	if retryQueue.Size() != 1 {
		t.Fatal("failed oneway was not retained for reconnect replay")
	}
	if s.IsOpen() {
		t.Error("failed connection was not closed")
	}
	frame := onewayPipeFrame(t, func(reconnected *TcpSession) bool {
		reconnected.RetryQueue = retryQueue
		// open() calls SendFailover before publishing its newly connected client.
		reconnected.client = nil
		return reconnected.SendFailover()
	})
	assertOnewayFrame(t, frame, 101, -42, p)
}

func TestOnewayFailoverPreservesDestination(t *testing.T) {
	q := onewayTestQueue(t, 2)
	p := onewayTestPack(101)
	if err := SendOneway(101, -42, p); err != nil {
		t.Fatal(err)
	}
	item := q.GetNoWait().(TcpSend)
	// A legacy payload sentinel makes accidental ordinary-frame fallback visible
	// as a wrong wire header instead of an unrelated nil-pack panic.
	item.pack = p
	oldMaster, oldSecurity := master, secSession
	master = &SecurityMaster{PCODE: 999, OID: 99}
	secSession = &SecuritySession{TRANSFER_KEY: 789}
	t.Cleanup(func() { master, secSession = oldMaster, oldSecurity })
	frame := onewayPipeFrame(t, func(s *TcpSession) bool {
		s.RetryQueue.PutForce(&item)
		s.client = nil
		return s.SendFailover()
	})
	assertOnewayFrame(t, frame, 101, -42, p)
}

func TestOnewaySnapshotsMutablePackPerProject(t *testing.T) {
	q := onewayTestQueue(t, 2)
	p := onewayTestPack(101)
	nested := value.NewMapValue()
	nested.PutString("detail", "before")
	p.Put("nested", nested)
	wantFirst := pack.ToPack(pack.ToBytesPack(p)).(*pack.ParamPack)
	if err := SendOneway(101, -42, p); err != nil {
		t.Fatal(err)
	}
	p.Pcode = 202
	p.PutString("stateName", "stopped")
	nested.PutString("detail", "after")
	wantSecond := pack.ToPack(pack.ToBytesPack(p)).(*pack.ParamPack)
	if err := SendOneway(202, 43, p); err != nil {
		t.Fatal(err)
	}
	p.Pcode, p.Id = 999, -1
	// Avoid golib v0.0.35's recursive Clear; overwrite values in place instead.
	p.PutString("instanceId", "i-mutated")
	p.PutLong("stateCode", 80)
	p.PutString("stateName", "terminated")
	nested.PutString("detail", "mutated again")
	for _, tt := range []struct {
		pcode int64
		hash  int64
		want  *pack.ParamPack
	}{{101, -42, wantFirst}, {202, 43, wantSecond}} {
		item := q.GetNoWait().(TcpSend)
		if item.pack != nil {
			t.Error("queue retained the caller's mutable pack")
		}
		frame := onewayPipeFrame(t, func(s *TcpSession) bool { return s.sendQueuedOneway(&item) })
		assertOnewayFrame(t, frame, tt.pcode, tt.hash, tt.want)
	}
}

func TestOnewayDoesNotUseNormalSecurityFlags(t *testing.T) {
	for _, level := range []int32{0, 128, 256} {
		for _, writeLock := range []bool{false, true} {
			t.Run(fmt.Sprintf("level_%d_lock_%t", level, writeLock), func(t *testing.T) {
				q := onewayTestQueue(t, 2)
				conf.CypherLevel = level
				conf.NetWriteLockEnabled = writeLock
				p := onewayTestPack(101)
				if err := SendOneway(101, -42, p); err != nil {
					t.Fatal(err)
				}
				item := q.GetNoWait().(TcpSend)
				// None of these ordinary flags belongs in the version byte.
				item.flag = NET_ONE_WAY_NO_CYPHER | NET_SECURE_CYPHER | NET_SECURE_HIDE
				frame := onewayPipeFrame(t, func(s *TcpSession) bool { return s.sendQueuedOneway(&item) })
				assertOnewayFrame(t, frame, 101, -42, p)
			})
		}
	}
}

func TestOnewayAdditionPreservesOrdinaryQueues(t *testing.T) {
	q := onewayTestQueue(t, 1)
	p := onewayTestPack(999)
	Send(NET_SECURE_CYPHER, p, false)
	Send(NET_SECURE_HIDE, onewayTestPack(888), true) // existing full-queue drop
	SendProfile(NET_SECURE_HIDE, p, true)
	first := q.GetNoWait().(TcpSend)
	second := q.GetNoWait().(TcpSend)
	if first.oneway != nil || first.pack != p || first.flag != NET_SECURE_CYPHER || first.flush {
		t.Error("ordinary primary queue behavior changed")
	}
	if second.oneway != nil || second.pack != p || second.flag != NET_SECURE_HIDE || !second.flush {
		t.Error("ordinary profile queue behavior changed")
	}
	if q.GetNoWait() != nil {
		t.Error("ordinary overflow policy changed")
	}
}
