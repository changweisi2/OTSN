// Package app implements the otsn subcommands.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"otsn/internal/events"
	"otsn/internal/snapshot"
	"otsn/internal/store"
	"otsn/internal/ui"
)

// Version is the otsn release version, overridable at build time with
// -ldflags "-X otsn/internal/app.Version=...".
var Version = "0.1.0"

// defaultKeep is how many snapshots the store retains automatically.
const defaultKeep = 48

// defaultExclude lists virtual filesystems that must never be scanned.
const defaultExclude = "/proc,/sys,/dev,/run"

// maxPathCol caps the path column width in tables.
const maxPathCol = 52

// ---- shared helpers ----------------------------------------------------

func parseRoots(args []string) ([]string, error) {
	roots := args
	if len(roots) == 0 {
		roots = defaultRoots()
	}
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		abs, err := filepath.Abs(r)
		if err != nil {
			return nil, err
		}
		out = append(out, filepath.Clean(abs))
	}
	return out, nil
}

func cleanExclude(list string) []string {
	var out []string
	for _, e := range strings.Split(list, ",") {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if abs, err := filepath.Abs(e); err == nil {
			out = append(out, filepath.Clean(abs))
		}
	}
	return out
}

// parseSize parses "10MB", "1.5G", "500K", "0".
func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" || s == "0" {
		return 0, nil
	}
	mult := int64(1)
	for _, u := range []struct {
		suf string
		m   int64
	}{
		{"TB", 1 << 40}, {"T", 1 << 40},
		{"GB", 1 << 30}, {"G", 1 << 30},
		{"MB", 1 << 20}, {"M", 1 << 20},
		{"KB", 1 << 10}, {"K", 1 << 10},
		{"B", 1},
	} {
		if strings.HasSuffix(s, u.suf) {
			mult = u.m
			s = strings.TrimSuffix(s, u.suf)
			break
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	return int64(v * float64(mult)), nil
}

func printJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func openStore() (*store.Store, error) {
	st, err := store.Open()
	if err != nil {
		return nil, fmt.Errorf("snapshot store: %w", err)
	}
	return st, nil
}

// scanOnce scans, archives, and prunes.
func scanOnce(st *store.Store, roots, exclude []string) (*snapshot.Snapshot, error) {
	snap, err := snapshot.Scan(roots, exclude, ui.Progress)
	if err != nil {
		return nil, err
	}
	ui.Done()
	if err := st.Save(snap); err != nil {
		return nil, err
	}
	if _, err := st.Prune(defaultKeep); err != nil {
		return nil, err
	}
	return snap, nil
}

func signDelta(d int64) string {
	if d >= 0 {
		return "+" + ui.FmtBytes(d)
	}
	return ui.FmtBytes(d)
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// ---- scan ---------------------------------------------------------------

// Scan implements 'otsn scan'.
func Scan(args []string) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "print machine-readable JSON")
	excl := fs.String("exclude", defaultExclude, "comma-separated path prefixes to skip")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: otsn scan [flags] [paths...]\n\nscan paths and store a snapshot; with no paths, scans the whole disk\n")
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
	prev, err := st.Latest()
	if err != nil {
		return err
	}
	start := time.Now()
	snap, err := scanOnce(st, roots, exclude)
	if err != nil {
		return err
	}
	elapsed := time.Since(start)

	if *jsonOut {
		out := map[string]any{
			"paths":    roots,
			"entries":  len(snap.Entries),
			"bytes":    snap.Total(),
			"elapsed":  elapsed.String(),
			"skipped":  snap.Skips,
			"snapshot": snap.Time.Format(time.RFC3339),
		}
		if total, used, err := diskUsage(roots[0]); err == nil {
			out["disk_total"] = total
			out["disk_used"] = used
			out["disk_free"] = total - used
		}
		if prev != nil {
			sum := snapshot.Summarize(snapshot.Diff(prev, snap))
			out["delta_bytes"] = sum.Delta
			out["files_changed"] = sum.Added + sum.Removed + sum.Changed
		}
		return printJSON(out)
	}

	fmt.Println()
	fmt.Println(ui.Title("scan complete"))
	fmt.Printf("  paths      %s\n", strings.Join(roots, ", "))
	if total, used, err := diskUsage(roots[0]); err == nil {
		fmt.Printf("  disk       %s used of %s (%.1f%%) · %s free\n",
			ui.FmtBytes(used), ui.FmtBytes(total), float64(used)/float64(total)*100,
			ui.FmtBytes(total-used))
	}
	fmt.Printf("  file size  %s\n", ui.FmtBytes(snap.Total()))
	fmt.Printf("  entries    %s\n", ui.FmtInt(int64(len(snap.Entries))))
	fmt.Printf("  elapsed    %s\n", elapsed.Round(time.Millisecond))
	if snap.Skips > 0 {
		fmt.Printf("  skipped    %s   (%s)\n", ui.FmtInt(snap.Skips), snap.Errors[0])
	}
	if prev != nil {
		sum := snapshot.Summarize(snapshot.Diff(prev, snap))
		fmt.Printf("  Δ          %s since %s  (%s files changed)\n",
			signDelta(sum.Delta), prev.Time.Format("2006-01-02 15:04"),
			ui.FmtInt(int64(sum.Added+sum.Removed+sum.Changed)))
	}
	if err := appendHistory(st, prev, snap); err != nil {
		ui.Warnf("history: %v", err)
	}
	fmt.Printf("  stored     %s\n", ui.Dim(st.Dir()))
	return nil
}

// appendHistory records one scan outcome for the web timeline, including
// the disk occupancy (df semantics) at scan time.
func appendHistory(st *store.Store, prev, snap *snapshot.Snapshot) error {
	e := store.HistoryEntry{Time: snap.Time, Total: snap.Total(), Roots: snap.Roots}
	if len(snap.Roots) > 0 {
		if total, used, err := diskUsage(snap.Roots[0]); err == nil {
			e.DiskTotal = total
			e.DiskUsed = used
		}
	}
	if prev != nil {
		sum := snapshot.Summarize(snapshot.Diff(prev, snap))
		e.Delta = sum.Delta
		e.Files = sum.Added + sum.Removed + sum.Changed
	}
	return st.AppendHistory(e)
}

// backfillHistory adds timeline entries for archived snapshots that
// predate the history log, so the web timeline shows every snapshot.
func backfillHistory(st *store.Store) error {
	snaps, err := st.List()
	if err != nil {
		return err
	}
	hist, err := st.History()
	if err != nil {
		return err
	}
	have := make(map[int64]bool, len(hist))
	for _, e := range hist {
		have[e.Time.UnixNano()] = true
	}
	for _, s := range snaps {
		if have[s.Time.UnixNano()] {
			continue
		}
		snap, err := st.Load(s)
		if err != nil {
			continue
		}
		if err := appendHistory(st, nil, snap); err != nil {
			return err
		}
	}
	return nil
}

// sameRoots reports whether two snapshots cover exactly the same roots,
// regardless of argument order.
func sameRoots(a, b *snapshot.Snapshot) bool {
	if len(a.Roots) != len(b.Roots) {
		return false
	}
	aa := slices.Clone(a.Roots)
	bb := slices.Clone(b.Roots)
	slices.Sort(aa)
	slices.Sort(bb)
	return slices.Equal(aa, bb)
}

// diffErr explains why two snapshots cannot be compared.
func diffErr(a, b *snapshot.Snapshot) error {
	return fmt.Errorf("snapshots cover different roots (%s vs %s) — diff would be meaningless; run 'otsn list' and pick a matching pair with --since",
		strings.Join(a.Roots, ","), strings.Join(b.Roots, ","))
}

// ---- watch ---------------------------------------------------------------

// Watch implements 'otsn watch'.
func Watch(args []string) error {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	interval := fs.Duration("interval", 10*time.Minute, "scan interval")
	alertMB := fs.Int64("alert", 0, "highlight growth of at least this many MB per scan")
	useEvents := fs.Bool("events", true, "scan early when filesystem events occur")
	jsonOut := fs.Bool("json", false, "emit one JSON object per scan (JSONL)")
	excl := fs.String("exclude", defaultExclude, "comma-separated path prefixes to skip")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: otsn watch [flags] [paths...]\n\nkeep scanning periodically and print growth as it happens\n")
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var trig *events.Trigger
	if *useEvents {
		if t, err := events.New(roots); err == nil {
			trig = t
		} else {
			ui.Warnf("event watching unavailable (%v); falling back to periodic scans only", err)
		}
	}
	if trig != nil {
		defer trig.Close()
	}

	note := ""
	if trig != nil {
		note = " · event-triggered"
	}
	fmt.Printf("%s watching %s · every %s%s\n",
		ui.Title("otsn"), ui.Abbrev(strings.Join(roots, ", ")), *interval, note)

	minGap := *interval / 5
	if minGap > 30*time.Second {
		minGap = 30 * time.Second
	}
	if minGap < time.Second {
		minGap = time.Second
	}

	var trigC <-chan struct{}
	if trig != nil {
		trigC = trig.C()
	}
	var (
		last     *snapshot.Snapshot
		lastScan time.Time
	)
	tick := time.NewTicker(*interval)
	defer tick.Stop()
	for {
		snap, err := scanOnce(st, roots, exclude)
		if err != nil {
			return err
		}
		lastScan = time.Now()
		if last != nil {
			reportDelta(snap, last, *alertMB, *jsonOut)
		}
		last = snap
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		case <-trigC:
			if time.Since(lastScan) < minGap {
				continue
			}
		}
	}
}

func reportDelta(snap, prev *snapshot.Snapshot, alertMB int64, jsonOut bool) {
	changes := snapshot.Diff(prev, snap)
	sum := snapshot.Summarize(changes)
	if jsonOut {
		var top []string
		for _, g := range snapshot.Aggregate(changes, 3) {
			if len(top) == 3 {
				break
			}
			top = append(top, g.Path)
		}
		_ = printJSON(map[string]any{
			"time":           snap.Time.Format(time.RFC3339),
			"delta_bytes":    sum.Delta,
			"files_added":    sum.Added,
			"files_removed":  sum.Removed,
			"files_modified": sum.Changed,
			"top":            top,
		})
		return
	}
	ts := ui.Dim(snap.Time.Format("15:04:05"))
	if sum.Delta == 0 {
		fmt.Printf("%s %s unchanged\n", ts, ui.Dim("Δ"))
		return
	}
	alert := alertMB > 0 && sum.Delta >= alertMB<<20
	mark := ui.Cyan("Δ")
	if alert {
		mark = ui.Red("▲")
	}
	var top string
	if groups := snapshot.Aggregate(changes, 3); len(groups) > 0 {
		g := groups[0]
		top = fmt.Sprintf("  top: %s (%s)", ui.Abbrev(g.Path), signDelta(g.Delta()))
	}
	line := fmt.Sprintf("%s %s %s  ·  %s files changed%s",
		ts, mark, signDelta(sum.Delta),
		ui.FmtInt(int64(sum.Added+sum.Removed+sum.Changed)), top)
	if alert {
		line += ui.Red(fmt.Sprintf("  ▲ growth ≥ %s", ui.FmtBytes(alertMB<<20)))
	}
	fmt.Println(line)
}

// ---- report ---------------------------------------------------------------

// Report implements 'otsn report'.
func Report(args []string) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	since := fs.String("since", "", "start snapshot: index from 'list', or a time prefix like 2025-01-01")
	all := fs.Bool("all", false, "summarize every adjacent pair of snapshots")
	depth := fs.Int("depth", 3, "aggregation depth (0 = per file)")
	minStr := fs.String("min", "10MB", "minimum change to list, e.g. 10MB (0 disables)")
	topN := fs.Int("top", 12, "maximum rows")
	jsonOut := fs.Bool("json", false, "print machine-readable JSON")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: otsn report [flags]\n\ndiff two snapshots: where and when disk space grew\n")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	minBytes, err := parseSize(*minStr)
	if err != nil {
		return err
	}
	st, err := openStore()
	if err != nil {
		return err
	}
	snaps, err := st.List()
	if err != nil {
		return err
	}
	if len(snaps) == 0 {
		return errors.New("no snapshots yet — run 'otsn scan' first")
	}
	if *all {
		return reportAll(st, snaps, *jsonOut)
	}
	to := snaps[len(snaps)-1]
	var from store.Snap
	if len(snaps) == 1 {
		from = snaps[0] // diff against itself: all zeros
	} else {
		from = snaps[len(snaps)-2]
	}
	if *since != "" {
		s, err := pickSnapshot(snaps, *since)
		if err != nil {
			return err
		}
		from = s
	}
	fromSnap, err := st.Load(from)
	if err != nil {
		return err
	}
	toSnap, err := st.Load(to)
	if err != nil {
		return err
	}
	return renderReport(fromSnap, toSnap, *depth, minBytes, *topN, *jsonOut)
}

func pickSnapshot(snaps []store.Snap, since string) (store.Snap, error) {
	if idx, err := strconv.Atoi(since); err == nil {
		if idx < 1 || idx > len(snaps) {
			return store.Snap{}, fmt.Errorf("snapshot index %d out of range (1..%d)", idx, len(snaps))
		}
		return snaps[idx-1], nil
	}
	for _, f := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(f, since, time.Local); err == nil {
			for _, s := range snaps {
				if !s.Time.Before(t) {
					return s, nil
				}
			}
			return store.Snap{}, fmt.Errorf("no snapshot at or after %s", since)
		}
	}
	return store.Snap{}, fmt.Errorf("cannot parse %q as index or time", since)
}

func renderReport(from, to *snapshot.Snapshot, depth int, minBytes int64, topN int, jsonOut bool) error {
	if !sameRoots(from, to) {
		return diffErr(from, to)
	}
	changes := snapshot.Diff(from, to)
	sum := snapshot.Summarize(changes)
	var groups []snapshot.Group
	for _, g := range snapshot.Aggregate(changes, depth) {
		d := g.Delta()
		if d < 0 {
			d = -d
		}
		if d >= minBytes {
			groups = append(groups, g)
		}
	}

	if jsonOut {
		if len(groups) > topN {
			groups = groups[:topN]
		}
		rows := make([]map[string]any, 0, len(groups))
		for _, g := range groups {
			rows = append(rows, map[string]any{
				"path": g.Path, "before": g.Before, "after": g.After, "delta": g.Delta(),
			})
		}
		return printJSON(map[string]any{
			"from":        from.Time.Format(time.RFC3339),
			"to":          to.Time.Format(time.RFC3339),
			"delta_bytes": sum.Delta,
			"files":       map[string]int{"added": sum.Added, "removed": sum.Removed, "modified": sum.Changed},
			"groups":      rows,
		})
	}

	fmt.Println()
	fmt.Println(ui.Title("otsn report"))
	fmt.Printf("  %s  →  %s\n", ui.Hi(from.Time.Format("2006-01-02 15:04")), ui.Hi(to.Time.Format("2006-01-02 15:04")))
	head := fmt.Sprintf("  Δ %s  ·  %s files changed  (+%d added, −%d removed, %d modified)",
		signDelta(sum.Delta), ui.FmtInt(int64(sum.Added+sum.Removed+sum.Changed)),
		sum.Added, sum.Removed, sum.Changed)
	if from.Total() > 0 {
		head += fmt.Sprintf("  (%+.1f%% of %s)", float64(sum.Delta)/float64(from.Total())*100, ui.FmtBytes(from.Total()))
	}
	fmt.Println(head)
	if len(groups) == 0 {
		fmt.Println(ui.Dim("  no significant changes at this depth"))
		return nil
	}
	fmt.Printf("  %s\n", ui.Dim(fmt.Sprintf("top changes · depth %d · min %s", depth, ui.FmtBytes(minBytes))))

	maxDelta := int64(1)
	for i, g := range groups {
		if i >= topN {
			break
		}
		d := g.Delta()
		if d < 0 {
			d = -d
		}
		if d > maxDelta {
			maxDelta = d
		}
	}
	rows := make([][]string, 0, min(topN, len(groups)))
	for i, g := range groups {
		if i >= topN {
			break
		}
		d := g.Delta()
		bar := ui.Bar(float64(abs64(d))/float64(maxDelta), 12)
		cell := signDelta(d)
		if d >= 0 {
			cell, bar = ui.Yellow(cell), ui.Yellow(bar)
		} else {
			cell, bar = ui.Green(cell), ui.Green(bar)
		}
		rows = append(rows, []string{
			clip(ui.Abbrev(g.Path), maxPathCol),
			ui.FmtBytes(g.Before),
			ui.FmtBytes(g.After),
			cell,
			bar,
		})
	}
	fmt.Print(ui.Table([]string{"path", "before", "after", "delta", ""}, rows, map[int]bool{1: true, 2: true, 3: true}))
	if len(groups) > topN {
		fmt.Printf("  %s\n", ui.Dim(fmt.Sprintf("… %d more (raise --top to see them)", len(groups)-topN)))
	}
	return nil
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

func reportAll(st *store.Store, snaps []store.Snap, jsonOut bool) error {
	type row struct {
		From, To time.Time
		Delta    int64
		Files    int
	}
	rows := make([]row, 0, len(snaps)-1)
	for i := 0; i+1 < len(snaps); i++ {
		a, err := st.Load(snaps[i])
		if err != nil {
			return err
		}
		b, err := st.Load(snaps[i+1])
		if err != nil {
			return err
		}
		if !sameRoots(a, b) {
			ui.Warnf("skipping interval %s → %s: snapshots cover different roots",
				snaps[i].Time.Format("01-02 15:04"), snaps[i+1].Time.Format("01-02 15:04"))
			continue
		}
		sum := snapshot.Summarize(snapshot.Diff(a, b))
		rows = append(rows, row{snaps[i].Time, snaps[i+1].Time, sum.Delta, sum.Added + sum.Removed + sum.Changed})
	}
	if jsonOut {
		out := make([]map[string]any, 0, len(rows))
		for _, r := range rows {
			out = append(out, map[string]any{
				"from": r.From.Format(time.RFC3339), "to": r.To.Format(time.RFC3339),
				"delta_bytes": r.Delta, "files_changed": r.Files,
			})
		}
		return printJSON(out)
	}
	fmt.Println()
	fmt.Println(ui.Title("otsn report — all intervals"))
	tbl := make([][]string, 0, len(rows))
	for _, r := range rows {
		tbl = append(tbl, []string{
			r.From.Format("01-02 15:04"), "→", r.To.Format("01-02 15:04"),
			signDelta(r.Delta), ui.FmtInt(int64(r.Files)),
		})
	}
	fmt.Print(ui.Table([]string{"from", "", "to", "Δ", "files"}, tbl, map[int]bool{3: true, 4: true}))
	return nil
}

// ---- top ---------------------------------------------------------------

// Top implements 'otsn top'.
func Top(args []string) error {
	fs := flag.NewFlagSet("top", flag.ContinueOnError)
	depth := fs.Int("depth", 2, "aggregation depth (0 = per file)")
	n := fs.Int("n", 20, "maximum rows")
	minStr := fs.String("min", "0", "minimum size to list")
	jsonOut := fs.Bool("json", false, "print machine-readable JSON")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: otsn top [flags]\n\nlargest directories in the latest snapshot\n")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	minBytes, err := parseSize(*minStr)
	if err != nil {
		return err
	}
	st, err := openStore()
	if err != nil {
		return err
	}
	snap, err := st.Latest()
	if err != nil {
		return err
	}
	if snap == nil {
		return errors.New("no snapshots yet — run 'otsn scan' first")
	}
	total := snap.Total()
	var groups []snapshot.Group
	for _, g := range snapshot.Top(snap, *depth) {
		if g.Size >= minBytes {
			groups = append(groups, g)
		}
	}

	if *jsonOut {
		if len(groups) > *n {
			groups = groups[:*n]
		}
		rows := make([]map[string]any, 0, len(groups))
		for _, g := range groups {
			rows = append(rows, map[string]any{"path": g.Path, "size": g.Size})
		}
		return printJSON(map[string]any{
			"snapshot": snap.Time.Format(time.RFC3339),
			"total":    total,
			"groups":   rows,
		})
	}

	fmt.Println()
	fmt.Println(ui.Title("otsn top"))
	fmt.Printf("  %s  ·  %s total  ·  %s entries\n",
		ui.Hi(snap.Time.Format("2006-01-02 15:04")), ui.FmtBytes(total), ui.FmtInt(int64(len(snap.Entries))))
	rows := make([][]string, 0, min(*n, len(groups)))
	for i, g := range groups {
		if i >= *n {
			break
		}
		share := 0.0
		if total > 0 {
			share = float64(g.Size) / float64(total)
		}
		rows = append(rows, []string{
			clip(ui.Abbrev(g.Path), maxPathCol),
			ui.FmtBytes(g.Size),
			fmt.Sprintf("%.1f%%", share*100),
			ui.Cyan(ui.Bar(share, 12)),
		})
	}
	fmt.Print(ui.Table([]string{"path", "size", "share", ""}, rows, map[int]bool{1: true, 2: true}))
	if len(groups) > *n {
		fmt.Printf("  %s\n", ui.Dim(fmt.Sprintf("… %d more (raise --n to see them)", len(groups)-*n)))
	}
	return nil
}

// ---- list / prune ---------------------------------------------------------

// List implements 'otsn list'.
func List(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "print machine-readable JSON")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: otsn list [flags]\n\nshow stored snapshots\n")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := openStore()
	if err != nil {
		return err
	}
	snaps, err := st.List()
	if err != nil {
		return err
	}
	if *jsonOut {
		out := make([]map[string]any, 0, len(snaps))
		rootsByTime := historyRoots(st)
		for i, s := range snaps {
			fi, _ := os.Stat(s.Path)
			out = append(out, map[string]any{
				"index": i + 1, "time": s.Time.Format(time.RFC3339), "bytes": fi.Size(),
				"roots": rootsByTime[s.Time.UnixNano()],
			})
		}
		return printJSON(out)
	}
	fmt.Println()
	fmt.Println(ui.Title("otsn list"))
	if len(snaps) == 0 {
		fmt.Println(ui.Dim("  no snapshots yet — run 'otsn scan' first"))
		return nil
	}
	rootsByTime := historyRoots(st)
	rows := make([][]string, 0, len(snaps))
	for i, s := range snaps {
		fi, _ := os.Stat(s.Path)
		roots := rootsByTime[s.Time.UnixNano()]
		label := "—"
		if len(roots) > 0 {
			label = clip(ui.Abbrev(strings.Join(roots, " ")), 40)
		}
		rows = append(rows, []string{
			strconv.Itoa(i + 1),
			s.Time.Local().Format("2006-01-02 15:04:05"),
			ui.FmtBytes(fi.Size()),
			label,
		})
	}
	fmt.Print(ui.Table([]string{"idx", "time", "size", "roots"}, rows, map[int]bool{0: true, 2: true}))
	fmt.Printf("  %s\n", ui.Dim(fmt.Sprintf("archive: %s", st.Dir())))
	return nil
}

// historyRoots maps snapshot times to the roots recorded for them, so
// 'list' can show each snapshot's scope without decoding every file.
// UnixNano keys keep timezone-laden time.Time values comparable.
func historyRoots(st *store.Store) map[int64][]string {
	hist, err := st.History()
	if err != nil {
		return nil
	}
	out := make(map[int64][]string, len(hist))
	for _, e := range hist {
		if _, ok := out[e.Time.UnixNano()]; !ok {
			out[e.Time.UnixNano()] = e.Roots
		}
	}
	return out
}

// Prune implements 'otsn prune'.
func Prune(args []string) error {
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	keep := fs.Int("keep", defaultKeep, "snapshots to keep")
	jsonOut := fs.Bool("json", false, "print machine-readable JSON")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: otsn prune [flags]\n\nremove old snapshots, keeping the --keep most recent\n")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := openStore()
	if err != nil {
		return err
	}
	removed, err := st.Prune(*keep)
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(map[string]any{"removed": removed, "kept": *keep})
	}
	fmt.Println()
	fmt.Println(ui.Title("otsn prune"))
	if len(removed) == 0 {
		fmt.Println(ui.Dim("  nothing to remove"))
		return nil
	}
	fmt.Printf("  removed %d snapshot(s):\n", len(removed))
	for _, p := range removed {
		fmt.Printf("    %s\n", ui.Dim(p))
	}
	return nil
}
