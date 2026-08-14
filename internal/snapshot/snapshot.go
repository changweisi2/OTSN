// Package snapshot models a scanned tree: what exists, how large it is,
// and when it last changed. Snapshots are immutable, sorted by path, and
// serialized with gob+gzip for compact on-disk storage.
package snapshot

import (
	"compress/gzip"
	"encoding/gob"
	"io"
	"sort"
	"time"
)

// Version is the snapshot format version.
const Version = 1

// Entry is one path in a snapshot: a file with its size, or a directory
// with its last-modified time. Directory mtimes are the subtree change
// signal that lets rescan skip unchanged trees entirely.
type Entry struct {
	Path  string
	Size  int64 // file size in bytes; 0 for directories
	MTime int64 // unix nanoseconds; for directories, subtree change signal
	Dir   bool
}

// Snapshot is a full listing of a scanned tree at a point in time.
// Entries is sorted by Path, which makes diffing and subtree lookups cheap.
type Snapshot struct {
	Version int
	Time    time.Time
	Roots   []string
	Entries []Entry
	Skips   int64    // entries that could not be read
	Errors  []string // first few read errors, for diagnostics
}

// New returns an empty snapshot stamped at t.
func New(roots []string, t time.Time) *Snapshot {
	return &Snapshot{Version: Version, Time: t, Roots: roots}
}

// Total returns the sum of all file sizes.
func (s *Snapshot) Total() int64 {
	var n int64
	for i := range s.Entries {
		if !s.Entries[i].Dir {
			n += s.Entries[i].Size
		}
	}
	return n
}

// Find returns the entry for path, if present.
func (s *Snapshot) Find(path string) (Entry, bool) {
	i := sort.Search(len(s.Entries), func(i int) bool { return s.Entries[i].Path >= path })
	if i < len(s.Entries) && s.Entries[i].Path == path {
		return s.Entries[i], true
	}
	return Entry{}, false
}

// Encode writes the snapshot to w as gzip-compressed gob.
func (s *Snapshot) Encode(w io.Writer) error {
	gz := gzip.NewWriter(w)
	if err := gob.NewEncoder(gz).Encode(s); err != nil {
		return err
	}
	return gz.Close()
}

// Decode reads a snapshot from r.
func Decode(r io.Reader) (*Snapshot, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	var s Snapshot
	if err := gob.NewDecoder(gz).Decode(&s); err != nil {
		return nil, err
	}
	return &s, nil
}
