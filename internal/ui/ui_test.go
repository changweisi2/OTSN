package ui

import "testing"

func TestFmtBytes(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{5 << 20, "5.0 MB"},
		{3 << 30, "3.0 GB"},
		{1 << 40, "1.0 TB"},
		{-1536, "-1.5 KB"},
	}
	for _, c := range cases {
		if got := FmtBytes(c.n); got != c.want {
			t.Errorf("FmtBytes(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestFmtInt(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1234, "1,234"},
		{1234567, "1,234,567"},
		{-1234567, "-1,234,567"},
	}
	for _, c := range cases {
		if got := FmtInt(c.n); got != c.want {
			t.Errorf("FmtInt(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestBar(t *testing.T) {
	if got := Bar(1, 4); got != "████" {
		t.Errorf("Bar(1, 4) = %q", got)
	}
	if got := Bar(0, 4); got != "····" {
		t.Errorf("Bar(0, 4) = %q", got)
	}
	if got := Bar(0.5, 4); got != "██··" {
		t.Errorf("Bar(0.5, 4) = %q", got)
	}
	if got := Bar(2, 4); got != "████" {
		t.Errorf("Bar(2, 4) = %q (clamped)", got)
	}
	if got := Bar(-1, 4); got != "····" {
		t.Errorf("Bar(-1, 4) = %q (clamped)", got)
	}
}

func TestTable(t *testing.T) {
	got := Table([]string{"a", "b"}, [][]string{{"x", "1"}, {"yy", "22"}}, map[int]bool{1: true})
	want := "┌────┬────┐\n│ a  │  b │\n├────┼────┤\n│ x  │  1 │\n│ yy │ 22 │\n└────┴────┘\n"
	if got != want {
		t.Errorf("Table = %q, want %q", got, want)
	}
}

func TestAbbrev(t *testing.T) {
	if got := Abbrev("/tmp/whatever"); got != "/tmp/whatever" {
		t.Errorf("Abbrev = %q", got)
	}
}
