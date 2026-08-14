// Package store manages the snapshot archive on disk.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"otsn/internal/snapshot"
)

const timeFmt = "20060102T150405.000000000Z"

// Snap describes one archived snapshot.
type Snap struct {
	Path string
	Time time.Time
}

// Store is a directory of archived snapshots, one file per snapshot.
type Store struct {
	dir string
}

// Open returns the snapshot store, creating it if needed. The directory is
// $OTSN_DIR if set, otherwise <user config dir>/otsn/snapshots.
func Open() (*Store, error) {
	dir := os.Getenv("OTSN_DIR")
	if dir == "" {
		cfg, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(cfg, "otsn")
	}
	dir = filepath.Join(dir, "snapshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Dir returns the archive directory.
func (s *Store) Dir() string { return s.dir }

// Save archives snap atomically (write to temp, rename into place).
func (s *Store) Save(snap *snapshot.Snapshot) error {
	name := snap.Time.UTC().Format(timeFmt) + ".snap.gz"
	path := filepath.Join(s.dir, name)
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := snap.Encode(f); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// List returns archived snapshots in chronological order.
func (s *Store) List() ([]Snap, error) {
	des, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var snaps []Snap
	for _, de := range des {
		name := de.Name()
		if de.IsDir() || !strings.HasSuffix(name, ".snap.gz") {
			continue
		}
		t, err := time.Parse(timeFmt, strings.TrimSuffix(name, ".snap.gz"))
		if err != nil {
			continue // foreign file; ignore
		}
		snaps = append(snaps, Snap{Path: filepath.Join(s.dir, name), Time: t})
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].Time.Before(snaps[j].Time) })
	return snaps, nil
}

// Latest returns the most recent snapshot, or nil if none exist.
func (s *Store) Latest() (*snapshot.Snapshot, error) {
	snaps, err := s.List()
	if err != nil {
		return nil, err
	}
	if len(snaps) == 0 {
		return nil, nil
	}
	return s.Load(snaps[len(snaps)-1])
}

// Load reads one archived snapshot.
func (s *Store) Load(snap Snap) (*snapshot.Snapshot, error) {
	f, err := os.Open(snap.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sn, err := snapshot.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", snap.Path, err)
	}
	return sn, nil
}

// Prune removes the oldest snapshots, keeping the most recent keep.
// It returns the paths removed.
func (s *Store) Prune(keep int) ([]string, error) {
	snaps, err := s.List()
	if err != nil {
		return nil, err
	}
	var removed []string
	for i := 0; i < len(snaps)-keep; i++ {
		if err := os.Remove(snaps[i].Path); err != nil {
			return removed, err
		}
		removed = append(removed, snaps[i].Path)
	}
	return removed, nil
}

// HistoryEntry is one scan's outcome, recorded so the web timeline can be
// rendered without decoding every snapshot file.
type HistoryEntry struct {
	Time      time.Time `json:"time"`
	Total     int64     `json:"total"`
	Delta     int64     `json:"delta"`
	Files     int       `json:"files"`
	Roots     []string  `json:"roots,omitempty"`
	DiskUsed  int64     `json:"disk_used,omitempty"`
	DiskTotal int64     `json:"disk_total,omitempty"`
}

func (s *Store) historyPath() string { return filepath.Join(s.dir, "history.jsonl") }

// AppendHistory records one scan outcome.
func (s *Store) AppendHistory(e HistoryEntry) error {
	f, err := os.OpenFile(s.historyPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

// History returns all recorded scan outcomes, oldest first.
func (s *Store) History() ([]HistoryEntry, error) {
	b, err := os.ReadFile(s.historyPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []HistoryEntry
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var e HistoryEntry
		if json.Unmarshal([]byte(line), &e) == nil {
			out = append(out, e)
		}
	}
	return out, nil
}
