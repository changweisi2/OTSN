package snapshot

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Scan walks roots and returns a snapshot of every entry beneath them.
//
// Every entry is read with Lstat only (never opened), and walking is
// parallelized across GOMAXPROCS*8 workers. A directory's mtime alone is
// not a reliable change signal — in-place writes and deep changes do not
// update ancestor directory mtimes — so Scan always re-reads the full
// tree. Excluded prefixes are skipped entirely.
//
// onProgress, if non-nil, is called periodically with the entry count.
func Scan(roots []string, exclude []string, onProgress func(int64)) (*Snapshot, error) {
	s := New(roots, time.Now())

	excluded := func(p string) bool {
		for _, e := range exclude {
			if p == e || strings.HasPrefix(p, e+string(filepath.Separator)) {
				return true
			}
		}
		return false
	}

	// Validate roots up front so a typo fails fast instead of producing
	// an empty snapshot.
	var rootJobs []string
	for _, r := range roots {
		if excluded(r) {
			continue
		}
		fi, err := os.Stat(r)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", r, err)
		}
		if !fi.IsDir() {
			return nil, fmt.Errorf("%s: not a directory", r)
		}
		rootJobs = append(rootJobs, r)
	}

	// A mutex+cond task queue instead of a bounded channel: enqueue can
	// never block, so a burst of high-fanout directories cannot deadlock
	// every worker on a full channel.
	var (
		mu      sync.Mutex
		cond    = sync.NewCond(&mu)
		todo    []string
		pending int
	)
	enqueue := func(dir string) {
		mu.Lock()
		todo = append(todo, dir)
		pending++
		cond.Signal()
		mu.Unlock()
	}
	dequeue := func() (string, bool) {
		mu.Lock()
		defer mu.Unlock()
		for len(todo) == 0 && pending > 0 {
			cond.Wait()
		}
		if pending == 0 {
			return "", false
		}
		dir := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		return dir, true
	}

	var (
		entriesMu sync.Mutex
		entries   []Entry
		done      atomic.Int64
		skips     atomic.Int64
		errMu     sync.Mutex
		errs      []string
	)
	skip := func(p string, err error) {
		skips.Add(1)
		errMu.Lock()
		if len(errs) < 8 {
			errs = append(errs, fmt.Sprintf("%s: %v", p, err))
		}
		errMu.Unlock()
	}
	add := func(e Entry) {
		entriesMu.Lock()
		entries = append(entries, e)
		entriesMu.Unlock()
		n := done.Add(1)
		if onProgress != nil && n%8192 == 0 {
			onProgress(n)
		}
	}
	visit := func(dir string) {
		fi, err := os.Lstat(dir)
		if err != nil {
			skip(dir, err)
			return
		}
		des, err := os.ReadDir(dir)
		if err != nil {
			skip(dir, err)
			return
		}
		add(Entry{Path: dir, MTime: fi.ModTime().UnixNano(), Dir: true})
		for _, de := range des {
			p := filepath.Join(dir, de.Name())
			fi, err := de.Info()
			if err != nil {
				fi, err = os.Lstat(p)
			}
			if err != nil {
				skip(p, err)
				continue
			}
			if excluded(p) {
				continue
			}
			if fi.IsDir() {
				enqueue(p)
			} else {
				add(Entry{Path: p, Size: fi.Size(), MTime: fi.ModTime().UnixNano()})
			}
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < runtime.GOMAXPROCS(0)*8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				dir, ok := dequeue()
				if !ok {
					return
				}
				visit(dir)
				mu.Lock()
				pending--
				if pending == 0 {
					cond.Broadcast()
				}
				mu.Unlock()
			}
		}()
	}
	for _, r := range rootJobs {
		enqueue(r)
	}
	wg.Wait()

	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	s.Entries = entries
	s.Skips = skips.Load()
	s.Errors = errs
	return s, nil
}
