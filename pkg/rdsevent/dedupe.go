package rdsevent

import (
	"sort"
)

// maxSeenPerCursor bounds the de-duplication memory of one (role, region)
// cursor. Normally only the events of the last overlap window are kept.
const maxSeenPerCursor = 10000

// seenSet remembers the events already handed to the sender while an
// overlapping query can still return them. Entries are keyed by eventKey and
// carry the event date so that the set can drop everything older than the
// next window's start.
type seenSet struct {
	max   int
	dates map[string]int64
}

func newSeenSet(max int) *seenSet {
	if max <= 0 {
		max = maxSeenPerCursor
	}
	return &seenSet{max: max, dates: make(map[string]int64)}
}

func (s *seenSet) has(key string) bool {
	_, ok := s.dates[key]
	return ok
}

func (s *seenSet) len() int {
	return len(s.dates)
}

func (s *seenSet) add(key string, dateMs int64) {
	s.dates[key] = dateMs
	if len(s.dates) > s.max {
		// Drop the oldest tenth at once so a storm does not sort on every add.
		s.keepNewest(s.max - s.max/10)
	}
}

// pruneBefore forgets events dated before ms: the next window starts at or
// after ms, so DescribeEvents cannot return them again.
func (s *seenSet) pruneBefore(ms int64) {
	for key, date := range s.dates {
		if date < ms {
			delete(s.dates, key)
		}
	}
}

func (s *seenSet) keepNewest(n int) {
	entries := s.entries()
	if len(entries) <= n {
		return
	}
	for _, e := range entries[:len(entries)-n] {
		delete(s.dates, e.Key)
	}
}

// entries returns the set ordered by date, then key, for a stable state file.
func (s *seenSet) entries() []seenEntry {
	out := make([]seenEntry, 0, len(s.dates))
	for key, date := range s.dates {
		out = append(out, seenEntry{Key: key, DateMs: date})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DateMs != out[j].DateMs {
			return out[i].DateMs < out[j].DateMs
		}
		return out[i].Key < out[j].Key
	})
	return out
}
