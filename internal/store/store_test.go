package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"otsn/internal/snapshot"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	t.Setenv("OTSN_DIR", t.TempDir())
	st, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestSaveListLoad(t *testing.T) {
	st := openTestStore(t)
	snap := snapshot.New([]string{"/"}, time.Now())
	snap.Entries = []snapshot.Entry{{Path: "/x", Size: 42}}

	if err := st.Save(snap); err != nil {
		t.Fatal(err)
	}
	snaps, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(snaps), 1; got != want {
		t.Fatalf("snapshots = %d, want %d", got, want)
	}
	back, err := st.Load(snaps[0])
	if err != nil {
		t.Fatal(err)
	}
	if back.Total() != 42 || len(back.Entries) != 1 {
		t.Errorf("loaded snapshot = %+v", back)
	}
	if !back.Time.Equal(snap.Time) {
		t.Errorf("time roundtrip = %v, want %v", back.Time, snap.Time)
	}
}

func TestPrune(t *testing.T) {
	st := openTestStore(t)
	for i := 0; i < 5; i++ {
		s := snapshot.New(nil, time.Now().Add(time.Duration(i)*time.Minute))
		if err := st.Save(s); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := st.Prune(2)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(removed), 3; got != want {
		t.Fatalf("removed = %d, want %d", got, want)
	}
	snaps, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(snaps), 2; got != want {
		t.Fatalf("remaining = %d, want %d", got, want)
	}
	for _, p := range removed {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s still exists", p)
		}
	}
}

func TestSaveAtomicNoTempLeftover(t *testing.T) {
	st := openTestStore(t)
	if err := st.Save(snapshot.New(nil, time.Now())); err != nil {
		t.Fatal(err)
	}
	des, err := os.ReadDir(st.Dir())
	if err != nil {
		t.Fatal(err)
	}
	for _, de := range des {
		if filepath.Ext(de.Name()) == ".tmp" {
			t.Errorf("temp file left behind: %s", de.Name())
		}
	}
}
