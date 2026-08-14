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

	jobs := make(chan string, runtime.GOMAXPROCS(0)*16)
	// pending tracks every queued directory; jobs is closed only once all
	// of them have been processed, so workers never send on a closed chan.
	var pending sync.WaitGroup
	var (
		mu      sync.Mutex
		entries []Entry
		done    atomic.Int64
		skips   atomic.Int64
		errMu   sync.Mutex
		errs    []string
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
		mu.Lock()
		entries = append(entries, e)
		mu.Unlock()
		n := done.Add(1)
		if onProgress != nil && n%8192 == 0 {
			onProgress(n)
		}
	}
	visit := func(dir string) {
		defer pending.Done()
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
				pending.Add(1)
				jobs <- p
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
			for dir := range jobs {
				visit(dir)
			}
		}()
	}
	pending.Add(len(rootJobs))
	for _, r := range rootJobs {
		jobs <- r
	}
	go func() {
		pending.Wait()
		close(jobs)
	}()
	wg.Wait()

	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	s.Entries = entries
	s.Skips = skips.Load()
	s.Errors = errs
	return s, nil
}
