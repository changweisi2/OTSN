package app

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"otsn/internal/events"
	"otsn/internal/snapshot"
	"otsn/internal/store"
	"otsn/internal/ui"
)

//go:embed web/index.html
var webFS embed.FS

// Serve implements 'otsn serve': a local web dashboard backed by a
// background scan loop. The latest snapshot is kept in memory so API
// responses never decode snapshot files.
func Serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8787", "listen address")
	interval := fs.Duration("interval", 10*time.Minute, "scan interval")
	useEvents := fs.Bool("events", true, "scan early when filesystem events occur")
	excl := fs.String("exclude", defaultExclude, "comma-separated path prefixes to skip")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: otsn serve [flags] [paths...]\n\nserve a local web dashboard of disk usage\n")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	roots, err := parseRoots(fs.Args())
	if err != nil {
		return err
	}
	exclude := cleanExclude(*excl)
	st, err := openStore()
	if err != nil {
		return err
	}
	go func() {
		if err := backfillHistory(st); err != nil {
			ui.Warnf("history backfill: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var (
		mu   sync.Mutex
		last *snapshot.Snapshot
	)
	if s, err := st.Latest(); err == nil {
		last = s
	}
	record := func(prev, snap *snapshot.Snapshot) {
		if err := appendHistory(st, prev, snap); err != nil {
			ui.Warnf("history: %v", err)
		}
	}
	go scanLoop(ctx, st, roots, exclude, *interval, *useEvents, &mu, &last, record)

	mux := serveMux(st, func() *snapshot.Snapshot {
		mu.Lock()
		defer mu.Unlock()
		return last
	})
	srv := &http.Server{Addr: *addr, Handler: mux}
	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()
	fmt.Printf("%s dashboard at %s · watching %s · every %s\n",
		ui.Title("otsn"), ui.Hi("http://"+*addr), strings.Join(roots, ", "), *interval)
	err = srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// scanLoop scans immediately, then on every tick or filesystem event
// (rate-limited), keeping last up to date and recording history.
func scanLoop(ctx context.Context, st *store.Store, roots, exclude []string,
	interval time.Duration, useEvents bool, mu *sync.Mutex, last **snapshot.Snapshot,
	record func(prev, snap *snapshot.Snapshot)) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	var trigC <-chan struct{}
	if useEvents {
		if t, err := events.New(roots); err == nil {
			defer t.Close()
			trigC = t.C()
		} else {
			ui.Warnf("event watching unavailable (%v); periodic scans only", err)
		}
	}
	minGap := interval / 5
	if minGap > 30*time.Second {
		minGap = 30 * time.Second
	}
	if minGap < time.Second {
		minGap = time.Second
	}
	var lastScan time.Time
	for {
		snap, err := scanOnce(st, roots, exclude)
		if err != nil {
			ui.Warnf("scan failed: %v", err)
		} else {
			mu.Lock()
			prev := *last
			*last = snap
			mu.Unlock()
			record(prev, snap)
			if prev != nil {
				sum := snapshot.Summarize(snapshot.Diff(prev, snap))
				fmt.Printf("%s %s %s  ·  %s files changed\n",
					ui.Dim(snap.Time.Format("15:04:05")), ui.Cyan("Δ"), signDelta(sum.Delta),
					ui.FmtInt(int64(sum.Added+sum.Removed+sum.Changed)))
			}
		}
		lastScan = time.Now()
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-trigC:
			if time.Since(lastScan) < minGap {
				continue
			}
		}
	}
}

func serveMux(st *store.Store, getLast func() *snapshot.Snapshot) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		b, err := webFS.ReadFile("web/index.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(b)
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		snap := getLast()
		hist, err := st.History()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out := map[string]any{
			"version":   Version,
			"roots":     rootsOf(snap),
			"scannedAt": timeOf(snap),
			"total":     totalOf(snap),
			"entries":   entriesOf(snap),
			"history":   len(hist),
			"skips":     skipsOf(snap),
			"disks":     diskStats(rootsOf(snap)),
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("/api/top", func(w http.ResponseWriter, r *http.Request) {
		snap := getLast()
		if snap == nil {
			writeJSON(w, map[string]any{"groups": []any{}})
			return
		}
		depth := intQuery(r, "depth", 2)
		n := intQuery(r, "n", 20)
		min, err := parseSize(r.URL.Query().Get("min"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		groups := snapshot.Top(snap, depth)
		rows := make([]map[string]any, 0, len(groups))
		for i, g := range groups {
			if i >= n || g.Size < min {
				continue
			}
			share := 0.0
			if total := snap.Total(); total > 0 {
				share = float64(g.Size) / float64(total)
			}
			rows = append(rows, map[string]any{
				"path": g.Path, "size": g.Size, "share": share,
			})
		}
		writeJSON(w, map[string]any{
			"total":  snap.Total(),
			"groups": rows,
		})
	})
	// extra cache: snapshots without a history entry are loaded on demand
	// so the timeline shows every snapshot (refreshed once a minute).
	var (
		extraMu   sync.Mutex
		extraAt   time.Time
		extraHist []store.HistoryEntry
	)
	loadExtra := func(hist []store.HistoryEntry) ([]store.HistoryEntry, error) {
		extraMu.Lock()
		defer extraMu.Unlock()
		if time.Since(extraAt) < time.Minute && extraHist != nil {
			return extraHist, nil
		}
		snaps, err := st.List()
		if err != nil {
			return nil, err
		}
		have := make(map[int64]bool, len(hist))
		for _, e := range hist {
			have[e.Time.UnixNano()] = true
		}
		out := make([]store.HistoryEntry, 0, len(snaps))
		for _, s := range snaps {
			if have[s.Time.UnixNano()] {
				continue
			}
			snap, err := st.Load(s)
			if err != nil {
				continue
			}
			e := store.HistoryEntry{Time: snap.Time, Total: snap.Total(), Roots: snap.Roots}
			if len(snap.Roots) > 0 {
				if total, used, err := diskUsage(snap.Roots[0]); err == nil {
					e.DiskTotal = total
					e.DiskUsed = used
				}
			}
			out = append(out, e)
		}
		extraHist = out
		extraAt = time.Now()
		return out, nil
	}
	mux.HandleFunc("/api/history", func(w http.ResponseWriter, r *http.Request) {
		hist, err := st.History()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		extra, err := loadExtra(hist)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		all := append(hist, extra...)
		sort.Slice(all, func(i, j int) bool { return all[i].Time.Before(all[j].Time) })
		writeJSON(w, map[string]any{"history": all})
	})
	mux.HandleFunc("/api/report", func(w http.ResponseWriter, r *http.Request) {
		snaps, err := st.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if len(snaps) == 0 {
			writeJSON(w, map[string]any{"groups": []any{}})
			return
		}
		var from store.Snap
		if len(snaps) == 1 {
			from = snaps[0] // diff against itself: all zeros
		} else {
			from = snaps[len(snaps)-2]
		}
		if since := r.URL.Query().Get("since"); since != "" {
			s, err := pickSnapshot(snaps, since)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			from = s
		}
		a, err := st.Load(from)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		b, err := st.Load(snaps[len(snaps)-1])
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !sameRoots(a, b) {
			http.Error(w, diffErr(a, b).Error(), http.StatusBadRequest)
			return
		}
		depth := intQuery(r, "depth", 3)
		n := intQuery(r, "top", 12)
		min, err := parseSize(r.URL.Query().Get("min"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		changes := snapshot.Diff(a, b)
		sum := snapshot.Summarize(changes)
		rows := make([]map[string]any, 0)
		for i, g := range snapshot.Aggregate(changes, depth) {
			if i >= n {
				break
			}
			d := g.Delta()
			if d < 0 {
				d = -d
			}
			if d < min {
				continue
			}
			rows = append(rows, map[string]any{
				"path": g.Path, "before": g.Before, "after": g.After, "delta": g.Delta(),
			})
		}
		writeJSON(w, map[string]any{
			"from": from.Time, "to": snaps[len(snaps)-1].Time,
			"deltaBytes": sum.Delta,
			"files":      map[string]int{"added": sum.Added, "removed": sum.Removed, "modified": sum.Changed},
			"groups":     rows,
		})
	})
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func intQuery(r *http.Request, key string, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(key)); err == nil {
		return v
	}
	return def
}

func rootsOf(s *snapshot.Snapshot) []string {
	if s == nil {
		return nil
	}
	return s.Roots
}

func timeOf(s *snapshot.Snapshot) *time.Time {
	if s == nil {
		return nil
	}
	return &s.Time
}

func totalOf(s *snapshot.Snapshot) int64 {
	if s == nil {
		return 0
	}
	return s.Total()
}

func entriesOf(s *snapshot.Snapshot) int {
	if s == nil {
		return 0
	}
	return len(s.Entries)
}

func skipsOf(s *snapshot.Snapshot) int64 {
	if s == nil {
		return 0
	}
	return s.Skips
}

// diskStats reports the capacity of the filesystem holding each root.
func diskStats(roots []string) []map[string]any {
	var out []map[string]any
	for _, r := range roots {
		total, used, err := diskUsage(r)
		if err != nil {
			continue
		}
		out = append(out, map[string]any{
			"path":  r,
			"total": total,
			"used":  used,
			"free":  total - used,
		})
	}
	return out
}
