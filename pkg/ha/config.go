package ha

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Default Lease parameters (recommended values from docs/ha_implementation_plan.md §2.1).
const (
	defaultLeaseNamespace     = "whatap"
	defaultLeaseName          = "openagent-leader"
	defaultLeaseDuration      = 15 * time.Second
	defaultLeaseRenewDeadline = 10 * time.Second
	defaultLeaseRetryPeriod   = 2 * time.Second
)

// ConfigSource is the minimal read-only interface this package needs from the
// process configuration. The whatap-config package's WhatapConfig satisfies it,
// but using an interface here keeps pkg/ha free of side-effecting imports
// (the whatap-config package's init creates files in CWD, which would pollute
// test runs of this package).
type ConfigSource interface {
	Get(key string) string
	GetWithDefault(key, def string) string
	GetIntWithDefault(key string, def int) int
	GetBoolWithDefault(key string, def bool) bool
}

// Config is the resolved HA configuration for the running process.
//
// All fields are populated from the supplied ConfigSource and environment
// variables via LoadConfig. A nil or zero-value Config is equivalent to
// ModeNone (HA disabled).
type Config struct {
	Mode Mode

	// Leader election settings (used when Mode == ModeLeaderElection).
	LeaseNamespace     string
	LeaseName          string
	LeaseDuration      time.Duration
	LeaseRenewDeadline time.Duration
	LeaseRetryPeriod   time.Duration
	WarmStandby        bool

	// Static shard settings (used when Mode == ModeStaticShard).
	// ShardIndex is 0-based; ShardTotal must be >= 1 and ShardIndex < ShardTotal.
	ShardIndex         int
	ShardTotal         int
	OnameSuffixEnabled bool

	// Identity is a stable identifier for this process instance, used as
	// the Lease holderIdentity in leader election. Falls back to hostname
	// or POD_NAME when not explicitly set.
	Identity string
}

// LoadConfig reads HA configuration from the supplied ConfigSource and from
// environment variables.
//
// Precedence for shard index/total: explicit env vars (SHARD_INDEX, SHARD_TOTAL)
// override config file values. This matches the StatefulSet ordinal injection pattern.
//
// LoadConfig validates the resulting configuration and returns an error when the
// settings are inconsistent (e.g. ModeStaticShard with ShardTotal < 1).
func LoadConfig(src ConfigSource) (*Config, error) {
	if src == nil {
		return nil, fmt.Errorf("ha: ConfigSource is nil")
	}

	cfg := &Config{
		Mode: ParseMode(src.Get("ha_mode")),

		LeaseNamespace:     src.GetWithDefault("ha_lease_namespace", defaultLeaseNamespace),
		LeaseName:          src.GetWithDefault("ha_lease_name", defaultLeaseName),
		LeaseDuration:      durationSeconds(src.GetIntWithDefault("ha_lease_duration_sec", int(defaultLeaseDuration/time.Second)), defaultLeaseDuration),
		LeaseRenewDeadline: durationSeconds(src.GetIntWithDefault("ha_lease_renew_deadline_sec", int(defaultLeaseRenewDeadline/time.Second)), defaultLeaseRenewDeadline),
		LeaseRetryPeriod:   durationSeconds(src.GetIntWithDefault("ha_lease_retry_period_sec", int(defaultLeaseRetryPeriod/time.Second)), defaultLeaseRetryPeriod),
		WarmStandby:        src.GetBoolWithDefault("ha_warm_standby", true),

		ShardIndex:         resolveShardIndex(src),
		ShardTotal:         resolveShardTotal(src),
		OnameSuffixEnabled: src.GetBoolWithDefault("ha_oname_suffix_enabled", true),

		Identity: resolveIdentity(),
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// validate checks the configuration for internal consistency.
func (c *Config) validate() error {
	switch c.Mode {
	case ModeNone:
		return nil

	case ModeLeaderElection:
		if c.LeaseDuration <= 0 {
			return fmt.Errorf("ha: ha_lease_duration_sec must be positive")
		}
		if c.LeaseRenewDeadline <= 0 || c.LeaseRenewDeadline >= c.LeaseDuration {
			return fmt.Errorf("ha: ha_lease_renew_deadline_sec must be > 0 and < ha_lease_duration_sec")
		}
		if c.LeaseRetryPeriod <= 0 || c.LeaseRetryPeriod >= c.LeaseRenewDeadline {
			return fmt.Errorf("ha: ha_lease_retry_period_sec must be > 0 and < ha_lease_renew_deadline_sec")
		}
		if c.LeaseNamespace == "" {
			return fmt.Errorf("ha: ha_lease_namespace must not be empty")
		}
		if c.LeaseName == "" {
			return fmt.Errorf("ha: ha_lease_name must not be empty")
		}
		if c.Identity == "" {
			return fmt.Errorf("ha: cannot determine instance identity (set HOSTNAME or POD_NAME)")
		}
		return nil

	case ModeStaticShard:
		if c.ShardTotal < 1 {
			return fmt.Errorf("ha: SHARD_TOTAL must be >= 1 for static_shard mode (got %d)", c.ShardTotal)
		}
		if c.ShardIndex < 0 || c.ShardIndex >= c.ShardTotal {
			return fmt.Errorf("ha: SHARD_INDEX must be in [0, %d) for static_shard mode (got %d)", c.ShardTotal, c.ShardIndex)
		}
		return nil

	default:
		return fmt.Errorf("ha: unknown ha_mode %q", c.Mode)
	}
}

// resolveShardIndex returns the shard index for this instance.
// Precedence: SHARD_INDEX env > ha_shard_index in config > ordinal parsed
// from POD_NAME (e.g. "openagent-2" -> 2) > 0.
func resolveShardIndex(src ConfigSource) int {
	if v := os.Getenv("SHARD_INDEX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	if n := src.GetIntWithDefault("ha_shard_index", -1); n >= 0 {
		return n
	}
	if podName := os.Getenv("POD_NAME"); podName != "" {
		if idx := strings.LastIndex(podName, "-"); idx >= 0 && idx < len(podName)-1 {
			if n, err := strconv.Atoi(podName[idx+1:]); err == nil {
				return n
			}
		}
	}
	return 0
}

// resolveShardTotal returns the total number of shards.
// Precedence: SHARD_TOTAL env > ha_shard_total in config > 1.
func resolveShardTotal(src ConfigSource) int {
	if v := os.Getenv("SHARD_TOTAL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return src.GetIntWithDefault("ha_shard_total", 1)
}

// resolveIdentity returns a stable identifier for this process instance.
// Used as the Lease holderIdentity in leader election.
func resolveIdentity() string {
	if v := os.Getenv("POD_NAME"); v != "" {
		return v
	}
	if v := os.Getenv("HOSTNAME"); v != "" {
		return v
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return ""
}

// durationSeconds converts an integer second count to a time.Duration,
// falling back to def when n <= 0.
func durationSeconds(n int, def time.Duration) time.Duration {
	if n <= 0 {
		return def
	}
	return time.Duration(n) * time.Second
}
