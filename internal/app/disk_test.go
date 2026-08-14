package app

import (
	"testing"
)

func TestDiskUsage(t *testing.T) {
	total, used, err := diskUsage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if total <= 0 {
		t.Errorf("total = %d, want > 0", total)
	}
	if used < 0 || used > total {
		t.Errorf("used = %d, want in [0, %d]", used, total)
	}
}
