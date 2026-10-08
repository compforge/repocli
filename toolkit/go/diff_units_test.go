package repocli_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/compforge/repocli/toolkit/go"
)

func TestDiffIsIndependentOfSyntaxAndComponentAnalysis(t *testing.T) {
	root := fixture(t)
	put(t, root, ".repocli.json", "invalid configuration")
	put(t, root, "main.go", "package main\nfunc Broken(\n")
	d, err := repocli.Diff(context.Background(), repocli.DiffRequest{Repository: root})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Complete || len(d.Changes) != 2 {
		t.Fatalf("pure diff unexpectedly analyzed source: %+v", d)
	}
	selected := d.Select("main.go")
	if len(selected.Changes) != 1 || len(d.Changes) != 2 {
		t.Fatal("selection mutated original diff")
	}
	r, err := repocli.FormUnits(context.Background(), selected, repocli.UnitOptions{FileOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Complete {
		t.Fatal("formation lost syntax diagnostics")
	}
}

func TestDiffSelectionAndFormationReuseCapturedSource(t *testing.T) {
	root := fixture(t)
	put(t, root, "main.go", "package main\nfunc Captured(){}\n")
	put(t, root, "other.go", "package main\nfunc Other(){}\n")
	d, err := repocli.Diff(context.Background(), repocli.DiffRequest{Repository: root})
	if err != nil {
		t.Fatal(err)
	}
	selected := d.Select("main.go")
	put(t, root, "main.go", "package main\nfunc Surprise(){}\n")
	// Formation must also work after the checkout disappears: all source and
	// unchanged manifest/dependency context belongs to the captured comparison.
	hidden := root + "-moved"
	if err := os.Rename(root, hidden); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(hidden)
	r, err := repocli.FormUnits(context.Background(), selected, repocli.UnitOptions{MaxUnits: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Units) != 1 || r.AfterSnapshot != d.AfterSnapshot {
		t.Fatalf("%+v", r)
	}
	for _, f := range r.Fragments {
		if f.Path != "main.go" || strings.Contains(f.Diff, "Surprise") || strings.Contains(f.Diff, "Other") {
			t.Fatalf("wrong captured target: %+v", f)
		}
	}
	source, err := selected.ReadSource(false, "main.go")
	if err != nil || !strings.Contains(source, "Captured") {
		t.Fatal(source, err)
	}
	empty, err := repocli.FormUnits(context.Background(), d.Select(), repocli.UnitOptions{})
	if err != nil || len(empty.Units) != 0 {
		t.Fatal(empty, err)
	}
}

func TestDiffRootCommitAndTypeChange(t *testing.T) {
	root := fixture(t)
	d, err := repocli.Diff(context.Background(), repocli.DiffRequest{Repository: root, EmptyBase: true, Head: "HEAD"})
	if err != nil || len(d.Changes) == 0 {
		t.Fatal(d, err)
	}
	for _, ch := range d.Changes {
		if !ch.IsNew {
			t.Fatalf("root commit has non-addition: %+v", ch)
		}
	}
	if err := os.Remove(filepath.Join(root, "main.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("elsewhere", filepath.Join(root, "main.go")); err != nil {
		t.Fatal(err)
	}
	d, err = repocli.Diff(context.Background(), repocli.DiffRequest{Repository: root})
	if err != nil {
		t.Fatal(err)
	}
	var removed, added bool
	for _, ch := range d.Changes {
		removed = removed || ch.IsDeleted
		added = added || ch.IsNew
	}
	if !removed || !added {
		t.Fatalf("type transition lost a side: %+v", d.Changes)
	}
}
