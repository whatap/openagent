package rdsevent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "rds_events_state.json")
	seen := newSeenSet(0)
	seen.add("k1", t0.Add(-time.Minute).UnixMilli())
	seen.add("k2", t0.UnixMilli())
	cursors := map[string]*cursor{
		"arn:aws:iam::111122223333:role/R|ap-northeast-2": {lastEnd: t0, seen: seen},
		"default|us-east-1": {lastEnd: t0.Add(-time.Hour), seen: newSeenSet(0)},
		"default|eu-west-1": newCursor(), // nothing collected yet: not written
	}
	if err := saveState(path, cursors); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state file mode %v, want 0600", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
	loaded, err := loadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded %d cursors", len(loaded))
	}
	got := loaded["arn:aws:iam::111122223333:role/R|ap-northeast-2"]
	if !got.lastEnd.Equal(t0) || !got.seen.has("k1") || !got.seen.has("k2") {
		t.Fatalf("cursor = %+v", got)
	}
	if !loaded["default|us-east-1"].lastEnd.Equal(t0.Add(-time.Hour)) {
		t.Fatal("second cursor lost")
	}
}

func TestMissingStateFileIsEmpty(t *testing.T) {
	cursors, err := loadState(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil || len(cursors) != 0 {
		t.Fatalf("cursors %v err %v", cursors, err)
	}
}

func TestUnreadableStateIsMovedAside(t *testing.T) {
	clock := &fakeClock{now: t0}
	cfg := testConfig(t, "ap-northeast-2")
	if err := os.WriteFile(cfg.StateFile, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	api := &fakeRDS{clock: clock}
	logs := &logCapture{}
	c := newTestCollector(&fakeFactory{apis: map[string]EventsAPI{"ap-northeast-2": api}}, (&recordingSink{}).send, clock, logs)
	res := c.RunCycle(context.Background(), cfg)
	if !res.Regions[0].Advanced {
		t.Fatalf("round after a corrupt state = %+v", res.Regions[0])
	}
	if got := api.lastCall(); !got.StartTime.Equal(t0.Add(-10 * time.Minute)) {
		t.Fatalf("start = %v, want initialLookback", got.StartTime)
	}
	matches, _ := filepath.Glob(cfg.StateFile + ".corrupt-*")
	if len(matches) != 1 || logs.count("state file unreadable") != 1 {
		t.Fatalf("corrupt file not quarantined: %v\n%s", matches, logs.all())
	}
	if _, err := loadState(cfg.StateFile); err != nil {
		t.Fatalf("new state unreadable: %v", err)
	}
}

func TestSeenSetPrunesAndBounds(t *testing.T) {
	s := newSeenSet(10)
	for i := 0; i < 25; i++ {
		s.add(strings.Repeat("k", i+1), int64(i))
	}
	if s.len() > 10 {
		t.Fatalf("set grew to %d", s.len())
	}
	if !s.has(strings.Repeat("k", 25)) {
		t.Fatal("newest entry evicted")
	}
	s.pruneBefore(23)
	if s.len() != 2 {
		t.Fatalf("after prune %d entries, want 2", s.len())
	}
}
