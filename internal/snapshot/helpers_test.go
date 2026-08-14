package snapshot

import (
	"sort"
	"time"
)

var zeroTime = time.Unix(0, 0)

func sortEntries(s *Snapshot) {
	sort.Slice(s.Entries, func(i, j int) bool { return s.Entries[i].Path < s.Entries[j].Path })
}
