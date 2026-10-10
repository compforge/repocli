package units

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestSameOwnerAdditionAndReplacement(t *testing.T) {
	old := "package p\nfunc F() {\n println(1)\n println(2)\n}\n"
	next := "package p\nfunc F() {\n println(3)\n println(2)\n println(4)\n}\n"
	ch := Change{OldPath: "a.go", NewPath: "a.go", OldFileContent: old, OldContentKnown: true, NewFileContent: next, BeforeRef: "old", AfterRef: "new", Insertions: 2, Deletions: 1,
		Diff: "@@ -1,5 +1,6 @@\n package p\n func F() {\n- println(1)\n+ println(3)\n println(2)\n+ println(4)\n }\n"}
	fs, err := Split(context.Background(), ch)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 2 {
		t.Fatalf("want two exact edit fragments: %+v", fs)
	}
	if err := ValidateEdits(ch, fs); err != nil {
		t.Fatal(err)
	}
	for _, withGraph := range []bool{false, true} {
		input := Input{Changes: []Change{ch}}
		if withGraph {
			input.After = graph(t, "new", map[string]string{"a.go": next})
		}
		r, err := Form(context.Background(), input)
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Units) != 1 || len(r.Units[0].FragmentIDs) != 2 || r.Units[0].Counts.After[Function] != 1 {
			t.Fatal(r)
		}
		if len(r.Decisions) != 1 || r.Decisions[0].Strategy != "same_owner" {
			t.Fatal(r.Decisions)
		}
		input.Options.MaxChangedLines = 2
		r, err = Form(context.Background(), input)
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Units) != 2 || r.Steps[0].BudgetBlocked == 0 {
			t.Fatalf("capacity must still cut same-owner edits: %+v", r)
		}
	}
	// Same spelling/range in another snapshot is not a shared declaration.
	fs[0].Before = nil
	fs[0].AfterRef = "different"
	r, err := Group(context.Background(), fs, nil, nil, Options{})
	if err != nil || len(r.Units) != 2 {
		t.Fatal(r, err)
	}
}

func TestFilePackingContinuesBelowCeiling(t *testing.T) {
	files := map[string]string{"a.go": "package p\nconst A = 1\nconst B = 2\nconst C = 3\n", "b.go": "package p\nconst D = 4\n"}
	fs := functionFragments(t, files)
	r, err := Group(context.Background(), fs, nil, nil, Options{MaxUnits: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Units) != 2 || unitForName(t, r, "A").ID != unitForName(t, r, "C").ID {
		t.Fatal(r)
	}
	slices.Reverse(fs)
	again, err := Group(context.Background(), fs, nil, nil, Options{MaxUnits: 20})
	if err != nil || !reflect.DeepEqual(r.Units, again.Units) || !reflect.DeepEqual(r.Decisions, again.Decisions) {
		t.Fatal("input order changed packing", again, err)
	}
}

func TestFilePackingPrefersNearbyDeclarationsUnderCapacity(t *testing.T) {
	files := map[string]string{"a.go": "package p\nfunc A(){}\n" + strings.Repeat("\n", 20) + "func B(){}\nfunc C(){}\n"}
	r, err := Group(context.Background(), functionFragments(t, files), nil, nil, Options{MaxUnits: 10, MaxChangedLines: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Units) != 2 || unitForName(t, r, "B").ID != unitForName(t, r, "C").ID || unitForName(t, r, "A").ID == unitForName(t, r, "B").ID {
		t.Fatal(r)
	}
}

func TestUnchangedSharedCalleeDoesNotGlueCallers(t *testing.T) {
	files := map[string]string{"go.mod": "module example\n", "a.go": "package p\nfunc A(){H()}\n", "b.go": "package p\nfunc B(){H()}\n", "helper.go": "package p\nfunc H(){}\n"}
	fs := functionFragments(t, map[string]string{"a.go": files["a.go"], "b.go": files["b.go"]})
	r, err := Group(context.Background(), fs, nil, graph(t, "new", files), Options{MaxUnits: 2})
	if err != nil || len(r.Units) != 2 {
		t.Fatal(r, err)
	}
}
