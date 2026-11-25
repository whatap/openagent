package rdsevent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const stateVersion = 1

// cursor is the collection position of one (roleArn, region) pair.
type cursor struct {
	// lastEnd is the EndTime of the last window whose events were all queued
	// for sending. Zero means no window yet: the first one uses initialLookback.
	lastEnd time.Time
	seen    *seenSet

	// Not persisted: consecutive throttled rounds and the earliest next try.
	throttled int
	nextTry   time.Time
	// failing remembers the error class of a failing region so the recovery is
	// logged once.
	failing bool
}

func newCursor() *cursor {
	return &cursor{seen: newSeenSet(maxSeenPerCursor)}
}

type persistedState struct {
	Version int                        `json:"version"`
	Cursors map[string]persistedCursor `json:"cursors"`
}

type persistedCursor struct {
	LastEndMs int64 `json:"lastEndMs"`
	// LastEnd repeats lastEndMs in RFC 3339 for people reading the file.
	LastEnd string      `json:"lastEnd,omitempty"`
	Seen    []seenEntry `json:"seen,omitempty"`
}

type seenEntry struct {
	Key    string `json:"key"`
	DateMs int64  `json:"dateMs"`
}

// cursorKey names the cursor of a credentials/region pair. A new role starts
// a new cursor; the default credential chain uses "default".
func cursorKey(roleARN, region string) string {
	if roleARN == "" {
		roleARN = "default"
	}
	return roleARN + "|" + region
}

// loadState reads the state file. A missing file is an empty state.
func loadState(path string) (map[string]*cursor, error) {
	cursors := make(map[string]*cursor)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cursors, nil
	}
	if err != nil {
		return cursors, err
	}
	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return cursors, fmt.Errorf("parse %s: %v", path, err)
	}
	if state.Version != stateVersion {
		return cursors, fmt.Errorf("parse %s: unsupported state version %d", path, state.Version)
	}
	for key, pc := range state.Cursors {
		c := newCursor()
		if pc.LastEndMs > 0 {
			c.lastEnd = time.UnixMilli(pc.LastEndMs)
		}
		for _, e := range pc.Seen {
			c.seen.add(e.Key, e.DateMs)
		}
		cursors[key] = c
	}
	return cursors, nil
}

// saveState writes the state atomically (temporary file, fsync, rename) so a
// crash leaves either the old or the new file.
func saveState(path string, cursors map[string]*cursor) error {
	state := persistedState{Version: stateVersion, Cursors: make(map[string]persistedCursor, len(cursors))}
	for key, c := range cursors {
		if c.lastEnd.IsZero() && c.seen.len() == 0 {
			continue
		}
		pc := persistedCursor{Seen: c.seen.entries()}
		if !c.lastEnd.IsZero() {
			pc.LastEndMs = c.lastEnd.UnixMilli()
			pc.LastEnd = c.lastEnd.UTC().Format(time.RFC3339Nano)
		}
		state.Cursors[key] = pc
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	tmpName = ""
	return nil
}

// quarantineState moves an unreadable state file aside so the collector can
// start fresh and the file stays available for inspection.
func quarantineState(path string, now time.Time) (string, error) {
	target := fmt.Sprintf("%s.corrupt-%s", path, now.UTC().Format("20060102T150405Z"))
	return target, os.Rename(path, target)
}
