package app

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"otsn/internal/snapshot"
	"otsn/internal/store"
)

func TestSameRoots(t *testing.T) {
	a := &snapshot.Snapshot{Roots: []string{"/a", "/b"}}
	b := &snapshot.Snapshot{Roots: []string{"/b", "/a"}}
	if !sameRoots(a, b) {
		t.Error("same set in different order must match")
	}
	c := &snapshot.Snapshot{Roots: []string{"/a"}}
	if sameRoots(a, c) {
		t.Error("different sets must not match")
	}
}

func TestReportRejectsMismatchedRoots(t *testing.T) {
	t.Setenv("OTSN_DIR", t.TempDir())
	st, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	s1 := snapshot.New([]string{"/alpha"}, time.Now().Add(-time.Minute))
	s2 := snapshot.New([]string{"/beta"}, time.Now())
	if err := st.Save(s1); err != nil {
		t.Fatal(err)
	}
	if err := st.Save(s2); err != nil {
		t.Fatal(err)
	}
	err = Report([]string{})
	if err == nil || !strings.Contains(err.Error(), "different roots") {
		t.Fatalf("Report err = %v, want roots mismatch", err)
	}
}

func TestReportOKWithMatchingRoots(t *testing.T) {
	t.Setenv("OTSN_DIR", t.TempDir())
	st, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	s1 := snapshot.New([]string{"/alpha"}, time.Now().Add(-time.Minute))
	s2 := snapshot.New([]string{"/alpha"}, time.Now())
	if err := st.Save(s1); err != nil {
		t.Fatal(err)
	}
	if err := st.Save(s2); err != nil {
		t.Fatal(err)
	}
	if err := Report([]string{}); err != nil {
		t.Fatalf("Report err = %v, want nil", err)
	}
}

func TestServeReportRejectsMismatchedRoots(t *testing.T) {
	t.Setenv("OTSN_DIR", t.TempDir())
	st, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	s1 := snapshot.New([]string{"/alpha"}, time.Now().Add(-time.Minute))
	s2 := snapshot.New([]string{"/beta"}, time.Now())
	if err := st.Save(s1); err != nil {
		t.Fatal(err)
	}
	if err := st.Save(s2); err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(serveMux(st, func() *snapshot.Snapshot { return s2 }))
	defer h.Close()
	r, err := h.Client().Get(h.URL + "/api/report")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 400 {
		t.Fatalf("status = %d, want 400", r.StatusCode)
	}
}
