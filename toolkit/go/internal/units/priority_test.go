package units

import (
	"context"
	"slices"
	"testing"
)

func functionFragments(t *testing.T, files map[string]string) []Fragment {
	t.Helper()
	var fs []Fragment
	for path, source := range files {
		if path == "go.mod" {
			continue
		}
		parts, err := Split(context.Background(), added(path, source))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range parts {
			if !f.Counts().Only(Unknown, Whitespace) {
				fs = append(fs, f)
			}
		}
	}
	return fs
}
func unitForName(t *testing.T, r Result, name string) Unit {
	t.Helper()
	for _, f := range r.Fragments {
		for _, e := range f.After {
			if e.Name == name {
				for _, u := range r.Units {
					if slices.Contains(u.FragmentIDs, f.ID) {
						return u
					}
				}
			}
		}
	}
	t.Fatalf("missing %s in %+v", name, r)
	return Unit{}
}

func TestContainmentBeforeCallsUnderBudget(t *testing.T) {
	files := map[string]string{"go.mod": "module example\n", "a.go": "package p\ntype C struct{}\nfunc (c C) M(){ H() }\nfunc H(){}\n"}
	r, err := Group(context.Background(), functionFragments(t, files), nil, graph(t, "new", files), Options{MaxChangedLines: 2})
	if err != nil {
		t.Fatal(err)
	}
	c, m, h := unitForName(t, r, "C"), unitForName(t, r, "C.M"), unitForName(t, r, "H")
	if c.ID != m.ID || m.ID == h.ID {
		t.Fatalf("contains must win before calls: %+v edges=%+v", r.Units, r.Relations)
	}
	if len(r.Decisions) == 0 || r.Decisions[0].Strategy != "contains" {
		t.Fatal(r.Decisions)
	}
}

func TestCrossFileCallsStopAtTarget(t *testing.T) {
	files := map[string]string{"go.mod": "module example\n", "a.go": "package p\nfunc A(){B()}\n", "b.go": "package p\nfunc B(){}\n", "c.go": "package p\nfunc C(){D()}\n", "d.go": "package p\nfunc D(){}\n"}
	r, err := Group(context.Background(), functionFragments(t, files), nil, graph(t, "new", files), Options{MaxUnits: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Units) != 3 || r.LimitExceeded {
		t.Fatal(r)
	}
	if len(r.Decisions) != 1 || r.Decisions[0].Strategy != "calls" {
		t.Fatal(r.Decisions)
	}
}

func TestFileFallbackPrecedesNamespaceAndRetainsImport(t *testing.T) {
	files := map[string]string{"go.mod": "module example\n", "a.go": "package p\nimport _ \"external\"\nfunc A(){}\n", "b.go": "package p\nfunc B(){}\n"}
	r, err := Group(context.Background(), functionFragments(t, files), nil, graph(t, "new", files), Options{MaxUnits: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Units) != 2 || len(r.Merges) != 0 {
		t.Fatal(r)
	}
	if len(r.Decisions) != 1 || r.Decisions[0].Strategy != "file" {
		t.Fatal(r.Decisions)
	}
	a := unitForName(t, r, "A")
	if len(a.FragmentIDs) != 2 || len(a.Paths) != 1 {
		t.Fatal(a)
	}
}

func TestNamespaceLeavesBeforeModuleRoot(t *testing.T) {
	files := map[string]string{"go.mod": "module example\n", "p/a.go": "package p\nfunc A(){}\n", "p/b.go": "package p\nfunc B(){}\n", "q/c.go": "package q\nfunc C(){}\n"}
	r, err := Group(context.Background(), functionFragments(t, files), nil, graph(t, "new", files), Options{MaxUnits: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Units) != 2 || len(r.Merges) != 1 {
		t.Fatal(r)
	}
	if unitForName(t, r, "A").ID != unitForName(t, r, "B").ID || unitForName(t, r, "A").ID == unitForName(t, r, "C").ID {
		t.Fatal(r.Units)
	}
}

func TestCountTargetCannotOverrideBudget(t *testing.T) {
	files := map[string]string{"go.mod": "module example\n", "a.go": "package p\nfunc A(){}\nfunc B(){}\n"}
	r, err := Group(context.Background(), functionFragments(t, files), nil, graph(t, "new", files), Options{MaxUnits: 1, MaxDiffSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Units) != 2 || !r.LimitExceeded {
		t.Fatal(r)
	}
}
