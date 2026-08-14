package snapshot

import (
	"path/filepath"
	"sort"
	"strings"
)

// Change is one file's delta between two snapshots.
type Change struct {
	Path    string
	Before  int64 // size in the older snapshot (0 if added)
	After   int64 // size in the newer snapshot (0 if removed)
	Added   bool  // file did not exist in the older snapshot
	Removed bool  // file no longer exists in the newer snapshot
}

// Delta returns After - Before.
func (c Change) Delta() int64 { return c.After - c.Before }

// Diff returns per-file changes from a (older) to b (newer), in path
// order. Directories are ignored: their space is fully described by the
// files beneath them.
func Diff(a, b *Snapshot) []Change {
	var out []Change
	i, j := 0, 0
	for i < len(a.Entries) && j < len(b.Entries) {
		ea, eb := a.Entries[i], b.Entries[j]
		switch {
		case ea.Path < eb.Path:
			if !ea.Dir {
				out = append(out, Change{Path: ea.Path, Before: ea.Size, Removed: true})
			}
			i++
		case ea.Path > eb.Path:
			if !eb.Dir {
				out = append(out, Change{Path: eb.Path, After: eb.Size, Added: true})
			}
			j++
		default: // same path
			if !ea.Dir && ea.Size != eb.Size {
				out = append(out, Change{Path: ea.Path, Before: ea.Size, After: eb.Size})
			}
			i++
			j++
		}
	}
	for ; i < len(a.Entries); i++ {
		if !a.Entries[i].Dir {
			out = append(out, Change{Path: a.Entries[i].Path, Before: a.Entries[i].Size, Removed: true})
		}
	}
	for ; j < len(b.Entries); j++ {
		if !b.Entries[j].Dir {
			out = append(out, Change{Path: b.Entries[j].Path, After: b.Entries[j].Size, Added: true})
		}
	}
	return out
}

// Summary aggregates a diff into totals.
type Summary struct {
	Delta   int64 // net bytes
	Added   int   // new files
	Removed int   // deleted files
	Changed int   // modified files
}

// Summarize totals a diff.
func Summarize(changes []Change) Summary {
	var s Summary
	for _, c := range changes {
		s.Delta += c.Delta()
		switch {
		case c.Added:
			s.Added++
		case c.Removed:
			s.Removed++
		default:
			s.Changed++
		}
	}
	return s
}

// Group is a path prefix with its aggregated sizes.
type Group struct {
	Path   string
	Before int64 // sum of sizes in the older snapshot (Aggregate)
	After  int64 // sum of sizes in the newer snapshot (Aggregate)
	Size   int64 // sum of sizes (Top)
}

// Delta returns After - Before.
func (g Group) Delta() int64 { return g.After - g.Before }

// Prefix returns the ancestor of path at the given depth, where depth 1 is
// the root's direct children (e.g. /home, /var). A depth of 0 returns the
// path unchanged. Paths shallower than the requested depth are returned
// as-is.
func Prefix(path string, depth int) string {
	if depth <= 0 {
		return path
	}
	vol := filepath.VolumeName(path)
	rest := strings.TrimLeft(strings.TrimPrefix(path, vol), string(filepath.Separator))
	parts := strings.Split(rest, string(filepath.Separator))
	if len(parts) <= depth {
		return path
	}
	joined := filepath.Join(parts[:depth]...)
	if vol == "" {
		return string(filepath.Separator) + joined
	}
	return filepath.Join(vol, joined)
}

// Aggregate buckets per-file deltas by path prefix and returns groups
// sorted by delta, largest first.
func Aggregate(changes []Change, depth int) []Group {
	m := make(map[string]int64, len(changes)/4)
	before := make(map[string]int64, len(changes)/4)
	for _, c := range changes {
		p := Prefix(c.Path, depth)
		m[p] += c.Delta()
		before[p] += c.Before
	}
	out := make([]Group, 0, len(m))
	for p, d := range m {
		out = append(out, Group{Path: p, Before: before[p], After: before[p] + d})
	}
	sortGroups(out, func(g Group) int64 { return g.Delta() })
	return out
}

// Top buckets file sizes by path prefix and returns groups sorted by
// size, largest first.
func Top(s *Snapshot, depth int) []Group {
	m := make(map[string]int64, len(s.Entries)/4)
	for _, e := range s.Entries {
		if e.Dir {
			continue
		}
		m[Prefix(e.Path, depth)] += e.Size
	}
	out := make([]Group, 0, len(m))
	for p, sz := range m {
		out = append(out, Group{Path: p, Size: sz})
	}
	sortGroups(out, func(g Group) int64 { return g.Size })
	return out
}

func sortGroups(groups []Group, key func(Group) int64) {
	sort.Slice(groups, func(i, j int) bool {
		if key(groups[i]) != key(groups[j]) {
			return key(groups[i]) > key(groups[j])
		}
		return groups[i].Path < groups[j].Path
	})
}
