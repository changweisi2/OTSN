package snapshot

import "testing"

func snapWith(t *testing.T, entries ...Entry) *Snapshot {
	t.Helper()
	s := New(nil, zeroTime)
	s.Entries = entries
	sortEntries(s)
	return s
}

func TestDiff(t *testing.T) {
	a := snapWith(t,
		Entry{Path: "/x", Size: 100},
		Entry{Path: "/y", Size: 50},
		Entry{Path: "/d", Dir: true},
		Entry{Path: "/d/f", Size: 10},
	)
	b := snapWith(t,
		Entry{Path: "/x", Size: 120},
		Entry{Path: "/z", Size: 30},
		Entry{Path: "/d", Dir: true},
		Entry{Path: "/d/f", Size: 10},
	)
	changes := Diff(a, b)
	if got, want := len(changes), 3; got != want {
		t.Fatalf("changes = %d, want %d", got, want)
	}
	if changes[0].Path != "/x" || changes[0].Delta() != 20 {
		t.Errorf("change 0 = %+v, want /x +20", changes[0])
	}
	if !changes[1].Removed || changes[1].Path != "/y" || changes[1].Delta() != -50 {
		t.Errorf("change 1 = %+v, want /y removed", changes[1])
	}
	if !changes[2].Added || changes[2].Path != "/z" || changes[2].Delta() != 30 {
		t.Errorf("change 2 = %+v, want /z added", changes[2])
	}
	sum := Summarize(changes)
	if sum.Added != 1 || sum.Removed != 1 || sum.Changed != 1 || sum.Delta != 0 {
		t.Errorf("summary = %+v", sum)
	}
}

func TestDiffEmpty(t *testing.T) {
	a := snapWith(t, Entry{Path: "/x", Size: 1})
	b := snapWith(t, Entry{Path: "/x", Size: 1})
	if got := Diff(a, b); len(got) != 0 {
		t.Errorf("expected no changes, got %+v", got)
	}
}

func TestPrefix(t *testing.T) {
	cases := []struct {
		path  string
		depth int
		want  string
	}{
		{"/home/cat/.cache", 1, "/home"},
		{"/home/cat/.cache", 2, "/home/cat"},
		{"/home/cat/.cache", 3, "/home/cat/.cache"},
		{"/home/cat/.cache", 4, "/home/cat/.cache"},
		{"/home/cat/.cache", 0, "/home/cat/.cache"},
		{"/", 2, "/"},
		{"/var/log", 1, "/var"},
	}
	for _, c := range cases {
		if got := Prefix(c.path, c.depth); got != c.want {
			t.Errorf("Prefix(%q, %d) = %q, want %q", c.path, c.depth, got, c.want)
		}
	}
}

func TestAggregate(t *testing.T) {
	a := snapWith(t,
		Entry{Path: "/home/a", Size: 10},
		Entry{Path: "/home/b", Size: 20},
		Entry{Path: "/var/log/x", Size: 5},
	)
	b := snapWith(t,
		Entry{Path: "/home/a", Size: 40},
		Entry{Path: "/home/b", Size: 25},
		Entry{Path: "/var/log/x", Size: 8},
	)
	groups := Aggregate(Diff(a, b), 1)
	if got, want := len(groups), 2; got != want {
		t.Fatalf("groups = %d, want %d (%+v)", got, want, groups)
	}
	g := groups[0]
	if g.Path != "/home" || g.Delta() != 35 || g.Before != 30 || g.After != 65 {
		t.Errorf("group = %+v", g)
	}
	g = groups[1]
	if g.Path != "/var" || g.Delta() != 3 || g.Before != 5 || g.After != 8 {
		t.Errorf("group = %+v", g)
	}
}

func TestTop(t *testing.T) {
	s := snapWith(t,
		Entry{Path: "/home/big", Size: 300},
		Entry{Path: "/home/small", Size: 100},
		Entry{Path: "/var/x", Size: 50},
	)
	groups := Top(s, 1)
	if got, want := len(groups), 2; got != want {
		t.Fatalf("groups = %d, want %d", got, want)
	}
	if groups[0].Path != "/home" || groups[0].Size != 400 {
		t.Errorf("groups[0] = %+v", groups[0])
	}
	if groups[1].Path != "/var" || groups[1].Size != 50 {
		t.Errorf("groups[1] = %+v", groups[1])
	}
}
