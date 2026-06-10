package ha

import (
	"fmt"
	"testing"
)

func TestSharder_Disabled(t *testing.T) {
	s := NewSharder(0, 1)
	if s.Enabled() {
		t.Fatal("expected sharder with total=1 to be disabled")
	}
	if !s.ShouldOwn("any-target") {
		t.Fatal("disabled sharder must own every target")
	}
}

func TestSharder_Bounds(t *testing.T) {
	tests := []struct {
		name       string
		idx, total int
		wantIdx    int
		wantTotal  int
	}{
		{"normal", 1, 3, 1, 3},
		{"zero total clamps to 1", 0, 0, 0, 1},
		{"negative total clamps to 1", 0, -5, 0, 1},
		{"negative index clamps to 0", -1, 3, 0, 3},
		{"index >= total clamps to total-1", 5, 3, 2, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSharder(tt.idx, tt.total)
			if s.Index() != tt.wantIdx || s.Total() != tt.wantTotal {
				t.Fatalf("got idx=%d total=%d, want idx=%d total=%d",
					s.Index(), s.Total(), tt.wantIdx, tt.wantTotal)
			}
		})
	}
}

func TestSharder_Partition(t *testing.T) {
	// Every target must be owned by exactly one shard.
	const total = 4
	const numTargets = 1000
	owners := make(map[string]int)

	for i := 0; i < numTargets; i++ {
		key := fmt.Sprintf("target-%d", i)
		ownerCount := 0
		for shard := 0; shard < total; shard++ {
			s := NewSharder(shard, total)
			if s.ShouldOwn(key) {
				owners[key] = shard
				ownerCount++
			}
		}
		if ownerCount != 1 {
			t.Fatalf("target %q owned by %d shards, want exactly 1", key, ownerCount)
		}
	}
}

func TestSharder_Distribution(t *testing.T) {
	// FNV-1a should distribute reasonably evenly across shards.
	// Allow ±20% deviation from the perfect average.
	const total = 4
	const numTargets = 10000
	counts := make([]int, total)

	for i := 0; i < numTargets; i++ {
		key := fmt.Sprintf("target-%d", i)
		for shard := 0; shard < total; shard++ {
			s := NewSharder(shard, total)
			if s.ShouldOwn(key) {
				counts[shard]++
			}
		}
	}

	expected := numTargets / total
	tolerance := expected / 5 // 20%
	for i, c := range counts {
		if c < expected-tolerance || c > expected+tolerance {
			t.Errorf("shard %d owns %d targets, want roughly %d (±%d)", i, c, expected, tolerance)
		}
	}
}

func TestSharder_Determinism(t *testing.T) {
	// Same key + same shard config must always return the same answer.
	s := NewSharder(2, 5)
	key := "stable-target-key"
	first := s.ShouldOwn(key)
	for i := 0; i < 100; i++ {
		if s.ShouldOwn(key) != first {
			t.Fatal("ShouldOwn is not deterministic for the same key")
		}
	}
}

func TestSharder_EmptyKey(t *testing.T) {
	// Empty keys go to shard 0 to keep the function total.
	for total := 1; total <= 5; total++ {
		for idx := 0; idx < total; idx++ {
			s := NewSharder(idx, total)
			got := s.ShouldOwn("")
			want := idx == 0 || total == 1
			if got != want {
				t.Errorf("shard %d/%d empty key: got %v, want %v", idx, total, got, want)
			}
		}
	}
}

func TestParseMode(t *testing.T) {
	tests := []struct {
		in   string
		want Mode
	}{
		{"", ModeNone},
		{"none", ModeNone},
		{"NONE", ModeNone},
		{"off", ModeNone},
		{"leader_election", ModeLeaderElection},
		{"Leader-Election", ModeLeaderElection},
		{"active_standby", ModeLeaderElection},
		{"static_shard", ModeStaticShard},
		{"sharding", ModeStaticShard},
		{"active-active", ModeStaticShard},
		{"unknown_mode", ModeNone},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := ParseMode(tt.in); got != tt.want {
				t.Errorf("ParseMode(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
