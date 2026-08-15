//go:build linux

package app

import "testing"

func TestUnescapeMount(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`/dev/sda1`, `/dev/sda1`},
		{`/mnt/my\040disk`, `/mnt/my disk`},
		{`/mnt/tab\011here`, "/mnt/tab\there"},
		{`no\escape`, `no\escape`},
		{`\040\040`, `  `},
	}
	for _, c := range cases {
		if got := unescapeMount(c.in); got != c.want {
			t.Errorf("unescapeMount(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
