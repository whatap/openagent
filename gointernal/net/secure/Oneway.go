package secure

import (
	"errors"
	"time"

	"github.com/whatap/golib/io"
	"github.com/whatap/golib/lang/pack"
)

// onewaySend owns an enqueue-time snapshot, never the caller's mutable pack.
// Keep routing next to the payload so reconnect replay cannot use agent identity.
type onewaySend struct {
	pcode       int64
	licenseHash int64
	payload     []byte
}

// SendOneway queues a raw ParamPack for a project over the existing collector
// connection. The sender must already be initialized with positive primary
// queue capacity and NetSendMaxBytes. Full queues reject without evicting prior
// entries or spilling into the profile queue. A nil error means local queue
// acceptance only, not delivery, authentication or remote execution; reconnect
// replay follows the existing best-effort retry policy and can duplicate data.
// The caller must not mutate p during this call; it may reuse p after return.
// The go-apm oneway protocol is unencrypted; normal session encryption is not
// applied to it. No new command ID is assigned by this transport.
func SendOneway(pcode int64, licenseHash int64, p *pack.ParamPack) (err error) {
	if pcode <= 0 || licenseHash == 0 {
		return errors.New("oneway requires a positive project and nonzero license hash")
	}
	if p == nil || p.Pcode != pcode || p.Id <= 0 || p.Request != 0 || p.Response != 0 {
		return errors.New("oneway requires a matching project, positive command and no request or response")
	}
	q := TcpQueue
	if q == nil || q.GetCapacity1() <= 0 {
		return errors.New("oneway requires an initialized bounded sender queue")
	}
	defer func() {
		if recover() != nil {
			// Pack codecs panic on malformed values. Do not expose those values.
			err = errors.New("oneway pack serialization failed")
		}
	}()
	item := TcpSend{flush: true, oneway: &onewaySend{
		pcode: pcode, licenseHash: licenseHash, payload: pack.ToBytesPack(p),
	}}
	if conf.NetSendMaxBytes <= 0 || len(item.oneway.payload) > int(conf.NetSendMaxBytes) {
		return errors.New("oneway pack exceeds the send limit")
	}
	if !q.Put1(item) {
		return errors.New("oneway sender queue is full")
	}
	return nil
}

// frame matches go-apm TcpSession.SendOneway: source, version (not a secure
// flag), destination pcode, signed license hash, then int-length raw pack bytes.
func (p *onewaySend) frame() []byte {
	out := io.NewDataOutputX()
	out.WriteByte(NETSRC_ONEWAY)
	out.WriteByte(0) // go-apm NET_SRC_AGENT_VERSION
	out.WriteLong(p.pcode)
	out.WriteLong(p.licenseHash)
	out.WriteIntBytes(p.payload)
	return out.ToByteArray()
}

// sendQueuedOneway is shared by the send worker and isolated transport tests.
func (s *TcpSession) sendQueuedOneway(item *TcpSend) (sent bool) {
	defer func() {
		if recover() != nil {
			// Never print payload, license hash, or panic values on this path.
			s.Close()
			sent = false
		}
	}()
	if conf.NetFailoverRetrySendDataEnabled {
		s.RetryQueue.PutForce(item)
	}
	if s.client == nil {
		return false
	}
	if err := s.client.SetWriteDeadline(time.Now().Add(time.Duration(conf.TcpSoSendTimeout) * time.Millisecond)); err != nil {
		s.Close()
		return false
	}
	err := s.writeOneway(item.oneway)
	if err == nil {
		_, err = s.Flush()
	}
	if err != nil {
		s.Close()
		return false
	}
	s.RetryQueue.Clear()
	return true
}

// The reconnect path has a writer before it publishes s.client. Reuse the same
// frame without requiring a live singleton, re-encrypting or replacing identity.
func (s *TcpSession) writeOneway(p *onewaySend) error {
	var err error
	if conf.NetWriteLockEnabled {
		_, err = s.WriteLock(p.frame())
	} else {
		_, err = s.Write(p.frame())
	}
	return err
}
