package app

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"otsn/internal/snapshot"
	"otsn/internal/store"
)

func testServeEnv(t *testing.T) (*store.Store, *snapshot.Snapshot) {
	t.Helper()
	t.Setenv("OTSN_DIR", t.TempDir())
	st, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.bin"), make([]byte, 1000), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := snapshot.Scan([]string{root}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Save(snap); err != nil {
		t.Fatal(err)
	}
	if err := appendHistory(st, nil, snap); err != nil {
		t.Fatal(err)
	}
	return st, snap
}

func getJSON(t *testing.T, h *httptest.Server, path string, v any) {
	t.Helper()
	r, err := h.Client().Get(h.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("%s: status %d", path, r.StatusCode)
	}
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		t.Fatal(err)
	}
}

func TestServeAPI(t *testing.T) {
	st, snap := testServeEnv(t)
	h := httptest.NewServer(serveMux(st, func() *snapshot.Snapshot { return snap }))
	defer h.Close()

	var status struct {
		Roots []string `json:"roots"`
		Total int64    `json:"total"`
		Hist  int      `json:"history"`
	}
	getJSON(t, h, "/api/status", &status)
	if status.Total != 1000 || status.Hist != 1 || len(status.Roots) != 1 {
		t.Errorf("status = %+v", status)
	}

	var top struct {
		Groups []struct {
			Path  string  `json:"path"`
			Size  int64   `json:"size"`
			Share float64 `json:"share"`
		} `json:"groups"`
	}
	getJSON(t, h, "/api/top?depth=1", &top)
	if len(top.Groups) != 1 || top.Groups[0].Size != 1000 || top.Groups[0].Share != 1 {
		t.Errorf("top = %+v", top)
	}

	var hist struct {
		History []store.HistoryEntry `json:"history"`
	}
	getJSON(t, h, "/api/history", &hist)
	if len(hist.History) != 1 || hist.History[0].Total != 1000 {
		t.Errorf("history = %+v", hist)
	}

	var rep struct {
		Delta int64 `json:"deltaBytes"`
	}
	getJSON(t, h, "/api/report", &rep)
	if rep.Delta != 0 { // diff against itself
		t.Errorf("report delta = %d, want 0", rep.Delta)
	}

	// The dashboard page itself must be served.
	r, err := h.Client().Get(h.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 200 || r.Header.Get("Content-Type") == "" {
		t.Errorf("index: status %d ct %q", r.StatusCode, r.Header.Get("Content-Type"))
	}
}

func TestHistoryRoundtrip(t *testing.T) {
	t.Setenv("OTSN_DIR", t.TempDir())
	st, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	want := []store.HistoryEntry{
		{Time: time.Now().Add(-time.Hour), Total: 10, Delta: 0, Files: 0},
		{Time: time.Now(), Total: 25, Delta: 15, Files: 3, DiskUsed: 1 << 30, DiskTotal: 200 << 30},
	}
	for _, e := range want {
		if err := st.AppendHistory(e); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.History()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("history = %d entries, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Total != want[i].Total || got[i].Delta != want[i].Delta ||
			got[i].Files != want[i].Files || got[i].DiskUsed != want[i].DiskUsed ||
			got[i].DiskTotal != want[i].DiskTotal {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// appendHistory must record real disk occupancy alongside scanned bytes.
func TestAppendHistoryRecordsDisk(t *testing.T) {
	t.Setenv("OTSN_DIR", t.TempDir())
	st, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	snap := snapshot.New([]string{t.TempDir()}, time.Now())
	if err := appendHistory(st, nil, snap); err != nil {
		t.Fatal(err)
	}
	hist, err := st.History()
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 {
		t.Fatalf("history = %d entries, want 1", len(hist))
	}
	if hist[0].DiskTotal <= 0 || hist[0].DiskUsed < 0 || hist[0].DiskUsed > hist[0].DiskTotal {
		t.Errorf("disk occupancy not recorded: %+v", hist[0])
	}
}

// Snapshots saved before the history log existed must get backfilled so
// the web timeline shows every snapshot.
func TestBackfillHistory(t *testing.T) {
	t.Setenv("OTSN_DIR", t.TempDir())
	st, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	snap := snapshot.New([]string{root}, time.Now().Add(-time.Hour))
	if err := st.Save(snap); err != nil {
		t.Fatal(err)
	}
	if err := backfillHistory(st); err != nil {
		t.Fatal(err)
	}
	hist, err := st.History()
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 {
		t.Fatalf("history = %d entries, want 1 backfilled", len(hist))
	}
	// Backfill must be idempotent.
	if err := backfillHistory(st); err != nil {
		t.Fatal(err)
	}
	hist, err = st.History()
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 {
		t.Fatalf("history = %d entries after second backfill, want 1", len(hist))
	}
}

// /api/history must not return duplicate points when the extra-cache is
// warm but a concurrent backfill has since added the same snapshot to
// history.jsonl.
func TestHistoryNoDuplicates(t *testing.T) {
	t.Setenv("OTSN_DIR", t.TempDir())
	st, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	snap := snapshot.New([]string{t.TempDir()}, time.Now())
	if err := st.Save(snap); err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(serveMux(st, func() *snapshot.Snapshot { return snap }))
	defer h.Close()

	// First request: no history entry, snapshot served via extra cache.
	var first struct {
		History []store.HistoryEntry `json:"history"`
	}
	getJSON(t, h, "/api/history", &first)
	if len(first.History) != 1 {
		t.Fatalf("first history = %d entries, want 1", len(first.History))
	}

	// Backfill now writes the same snapshot into history.jsonl, while
	// the extra cache is still warm.
	if err := appendHistory(st, nil, snap); err != nil {
		t.Fatal(err)
	}
	var second struct {
		History []store.HistoryEntry `json:"history"`
	}
	getJSON(t, h, "/api/history", &second)
	if len(second.History) != 1 {
		t.Fatalf("second history = %d entries, want 1 (no duplicates)", len(second.History))
	}
}

// snapSize returns -1 when the file is missing instead of crashing.
func TestSnapSizeMissing(t *testing.T) {
	if got := snapSize("/definitely/not/here.snap.gz"); got != -1 {
		t.Errorf("snapSize = %d, want -1", got)
	}
}

// Old history rows without disk fields must be approx-filled so the
// timeline shows every snapshot, with the estimate flagged.
func TestHistoryApproxFill(t *testing.T) {
	t.Setenv("OTSN_DIR", t.TempDir())
	st, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	old := store.HistoryEntry{Time: time.Now().Add(-time.Hour), Total: 10}
	if err := st.AppendHistory(old); err != nil {
		t.Fatal(err)
	}
	snap := snapshot.New([]string{t.TempDir()}, time.Now())
	if err := st.Save(snap); err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(serveMux(st, func() *snapshot.Snapshot { return snap }))
	defer h.Close()

	var resp struct {
		History []store.HistoryEntry `json:"history"`
	}
	getJSON(t, h, "/api/history", &resp)
	if len(resp.History) != 2 {
		t.Fatalf("history = %d entries, want 2", len(resp.History))
	}
	for _, e := range resp.History {
		if e.Total == 10 { // the pre-disk row
			if e.DiskTotal == 0 || e.DiskUsed <= 0 || !e.DiskApprox {
				t.Errorf("old row not approx-filled: %+v", e)
			}
			return
		}
	}
	t.Fatal("pre-disk row missing from response")
}
