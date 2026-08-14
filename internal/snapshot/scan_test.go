package snapshot

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func tree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a", "x.txt"), make([]byte, 10))
	writeFile(t, filepath.Join(root, "a", "y.txt"), make([]byte, 5))
	writeFile(t, filepath.Join(root, "b", "z.txt"), make([]byte, 7))
	return root
}

func TestScanBasics(t *testing.T) {
	root := tree(t)
	s, err := Scan([]string{root}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := s.Total(), int64(22); got != want {
		t.Errorf("Total = %d, want %d", got, want)
	}
	// root + a + b + 3 files
	if got, want := len(s.Entries), 6; got != want {
		t.Errorf("entries = %d, want %d", got, want)
	}
	for _, e := range s.Entries {
		if e.Dir == false {
			continue
		}
		if e.Size != 0 {
			t.Errorf("dir %s has nonzero size %d", e.Path, e.Size)
		}
	}
}

// A scan must see every kind of change: new files, deleted files, and
// in-place growth deep inside the tree.
func TestScanSeesAllChanges(t *testing.T) {
	root := tree(t)
	first, err := Scan([]string{root}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "b", "z.txt")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "c", "n.txt"), make([]byte, 100))
	writeFile(t, filepath.Join(root, "a", "x.txt"), make([]byte, 200)) // in-place growth
	second, err := Scan([]string{root}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := second.Total(), int64(305); got != want {
		t.Errorf("Total = %d, want %d", got, want)
	}
	if _, ok := second.Find(filepath.Join(root, "b", "z.txt")); ok {
		t.Error("deleted file still present")
	}
	if e, ok := second.Find(filepath.Join(root, "a", "x.txt")); !ok || e.Size != 200 {
		t.Errorf("grown file missing or wrong: %+v ok=%v", e, ok)
	}
	if got, want := Summarize(Diff(first, second)).Delta, int64(283); got != want {
		t.Errorf("diff delta = %d, want %d", got, want)
	}
}

func TestScanExclude(t *testing.T) {
	root := tree(t)
	s, err := Scan([]string{root}, []string{filepath.Join(root, "a")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := s.Total(), int64(7); got != want {
		t.Errorf("Total = %d, want %d", got, want)
	}
	if _, ok := s.Find(filepath.Join(root, "a", "x.txt")); ok {
		t.Error("excluded file still present")
	}
}

func TestScanRejectsMissingRoot(t *testing.T) {
	_, err := Scan([]string{filepath.Join(t.TempDir(), "nope")}, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing root")
	}
}

func TestScanSkipsSymlinkedDirs(t *testing.T) {
	if os.Getenv("CI") == "" && os.Getuid() == 0 {
		t.Skip("symlink test unreliable as root")
	}
	root := tree(t)
	target := filepath.Join(root, "a")
	link := filepath.Join(root, "loop")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	// Without cycle protection this would walk forever.
	s, err := Scan([]string{link}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := s.Total(), int64(15); got != want {
		t.Errorf("Total = %d, want %d (symlink must not be followed)", got, want)
	}
}
