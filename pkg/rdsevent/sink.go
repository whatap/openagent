package rdsevent

import (
	"errors"
	"time"

	"github.com/whatap/gointernal/net/secure"
	"github.com/whatap/golib/lang/pack"
)

const (
	// queueWaitLimit is how long one pack may wait for room in the send queue
	// before the round gives up and keeps the window for the next round.
	queueWaitLimit = 30 * time.Second
	queuePoll      = 50 * time.Millisecond
)

var (
	errNotConnected = errors.New("collector session not ready (pcode or oid unknown)")
	errQueueFull    = errors.New("send queue stayed full")
)

// sessionReady reports whether the agent has its project and object id, which
// every pack must carry.
func sessionReady() bool {
	secu := secure.GetSecurityMaster()
	return secu != nil && secu.PCODE != 0 && secu.OID != 0
}

// secureSink stamps the agent's identity on the pack, as the counter manager
// does for its TagCountPacks, and queues it on the shared collector session.
// The pack goes to this agent's own project.
func secureSink(p *pack.TagCountPack) error {
	secu := secure.GetSecurityMaster()
	if secu == nil || secu.PCODE == 0 || secu.OID == 0 {
		return errNotConnected
	}
	if !waitForQueueRoom(queueWaitLimit) {
		return errQueueFull
	}
	p.Pcode = secu.PCODE
	p.Oid = secu.OID
	p.Okind = secu.OKIND
	p.Onode = secu.ONODE
	secure.Send(secure.NET_SECURE_HIDE, p, true)
	return nil
}

// waitForQueueRoom waits until the first send queue is below three quarters of
// its capacity. secure.Send drops a pack silently when that queue is full, and
// a catch-up round can produce hundreds of event packs at once.
func waitForQueueRoom(limit time.Duration) bool {
	q := secure.TcpQueue
	if q == nil {
		return true
	}
	capacity := q.GetCapacity1()
	if capacity <= 0 {
		return true
	}
	threshold := capacity * 3 / 4
	if threshold < 1 {
		threshold = 1
	}
	deadline := time.Now().Add(limit)
	for q.Size1() >= threshold {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(queuePoll)
	}
	return true
}
