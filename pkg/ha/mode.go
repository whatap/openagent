package ha

import "strings"

// Mode represents the HA operation mode.
type Mode string

const (
	// ModeNone disables HA. The agent runs as a single instance (default).
	ModeNone Mode = "none"

	// ModeLeaderElection enables Active-Standby HA via Kubernetes Lease.
	// One pod is leader and performs all scraping; others stand by.
	// K8s environment only.
	ModeLeaderElection Mode = "leader_election"

	// ModeStaticShard enables Active-Active sharding via env-injected shard index.
	// Each instance handles hash(target) % SHARD_TOTAL == SHARD_INDEX.
	// Works in both K8s (StatefulSet) and standalone environments.
	ModeStaticShard Mode = "static_shard"
)

// ParseMode parses a mode string. Empty or unknown values yield ModeNone.
// Accepts case-insensitive variants and common aliases.
func ParseMode(s string) Mode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "none", "off", "disabled":
		return ModeNone
	case "leader_election", "leader-election", "leader", "active_standby", "active-standby":
		return ModeLeaderElection
	case "static_shard", "static-shard", "shard", "sharding", "active_active", "active-active":
		return ModeStaticShard
	default:
		return ModeNone
	}
}

// IsValid reports whether m is a recognized mode.
func (m Mode) IsValid() bool {
	switch m {
	case ModeNone, ModeLeaderElection, ModeStaticShard:
		return true
	default:
		return false
	}
}

// String returns the canonical string form of the mode.
func (m Mode) String() string {
	if !m.IsValid() {
		return string(ModeNone)
	}
	return string(m)
}
