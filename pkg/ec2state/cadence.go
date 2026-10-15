package ec2state

import (
	"container/list"
	"sync"
	"time"
)

const maxCachedDeliveries = 16384

// Forwarder owns one processor's delivery history. Its zero value is ready to use.
// Keep it across scrapes; a new Forwarder starts with no accepted observations.
// Do not copy a Forwarder after use. Enqueue must not re-enter this Forwarder.
type Forwarder struct {
	mu    sync.Mutex
	sent  map[deliveryKey]*sentState
	order list.List
	now   func() time.Time
}

type deliveryKey struct {
	sourceURL   string
	pcode       int64
	licenseHash int64
	senderOID   int32
	commandID   int32
	instanceID  string
}

// prune forgets removed routes and observations absent from this valid source
// snapshot. It never emits an observation or treats absence as termination.
func (f *Forwarder) prune(sources map[string]bool, destinations []destination, sourceURL string, observations []observation, senderOID, commandID int32) {
	activeDestinations := make(map[int64]int64, len(destinations))
	for _, destination := range destinations {
		activeDestinations[destination.pcode] = destination.licenseHash
	}
	present := make(map[string]bool, len(observations))
	for _, observation := range observations {
		present[observation.instanceID] = true
	}
	for key := range f.sent {
		removedSource := sources != nil && !sources[key.sourceURL]
		removedRoute := removedSource || activeDestinations[key.pcode] != key.licenseHash || key.senderOID != senderOID || key.commandID != commandID
		absentObservation := key.sourceURL == sourceURL && !present[key.instanceID]
		if removedRoute || absentObservation {
			f.forget(key)
		}
	}
}

func (f *Forwarder) reset() {
	f.sent = nil
	f.order.Init()
}

func (f *Forwarder) forget(key deliveryKey) {
	f.order.Remove(f.sent[key].position)
	delete(f.sent, key)
}

// Eviction can cause an extra send, never suppress a previously unseen state.
func (f *Forwarder) remember(key deliveryKey, state sentState) {
	if previous, exists := f.sent[key]; exists {
		state.position = previous.position
		f.order.MoveToFront(state.position)
	} else {
		if len(f.sent) >= maxCachedDeliveries {
			f.forget(f.order.Back().Value.(deliveryKey))
		}
		state.position = f.order.PushFront(key)
	}
	f.sent[key] = &state
}

type sentState struct {
	stateCode    int64
	stateName    string
	lastAccepted time.Time
	position     *list.Element
}
