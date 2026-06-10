package ha

import "hash/fnv"

// Sharder decides whether a given target belongs to this instance's shard.
//
// The mapping uses FNV-1a (64-bit) hashing modulo the shard count, giving a
// uniform distribution while keeping zero external dependencies. When a
// rebalance is triggered (SHARD_TOTAL change, instance restart with new index),
// targets are deterministically reassigned by all instances using the same
// hash function, so the cluster converges without coordination.
type Sharder struct {
	index int
	total int
}

// NewSharder constructs a Sharder. A total of 1 (or less) yields a passthrough
// sharder that owns every target — useful when the caller does not yet know
// whether sharding is enabled and wants a no-op default.
func NewSharder(index, total int) Sharder {
	if total < 1 {
		total = 1
	}
	if index < 0 {
		index = 0
	}
	if index >= total {
		index = total - 1
	}
	return Sharder{index: index, total: total}
}

// Index returns this sharder's index (0-based).
func (s Sharder) Index() int { return s.index }

// Total returns the total number of shards.
func (s Sharder) Total() int { return s.total }

// Enabled reports whether sharding is active (Total > 1).
// When disabled, ShouldOwn always returns true.
func (s Sharder) Enabled() bool { return s.total > 1 }

// ShouldOwn reports whether the given target key belongs to this shard.
//
// The key should be a stable identifier of the target (e.g. target.ID or
// "{job}|{address}") so that the assignment does not flap between scrape
// cycles. Empty keys are owned by shard 0 to keep the function total.
func (s Sharder) ShouldOwn(key string) bool {
	if !s.Enabled() {
		return true
	}
	if key == "" {
		return s.index == 0
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return int(h.Sum64()%uint64(s.total)) == s.index
}
