package repocli_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	cg "github.com/compforge/codegraph"
	"github.com/compforge/repocli/toolkit/go"
)

func TestDiffTagsUseChangePathAndRetainOpaqueChanges(t *testing.T) {
	root := fixture(t)
	put(t, root, "old.pb.go", "package main\nfunc OldGeneratedFunctionWithUniqueName(){}\n")
	put(t, root, "fixtures/remove.txt", "old")
	git(t, root, "add", ".")
	git(t, root, "commit", "-qm", "tagged fixtures")
	git(t, root, "mv", "old.pb.go", "plain.go")
	git(t, root, "rm", "fixtures/remove.txt")
	put(t, root, "fixtures/binary.bin", "a\x00b")
	put(t, root, "dist/large.min.js", strings.Repeat("x", (2<<20)+1))
	put(t, root, "kitex_gen/new.go", "package service\nfunc Generated(){}\n")
	d, err := repocli.Diff(context.Background(), repocli.DiffRequest{Repository: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Changes) != 5 {
		t.Fatalf("lost changes: %d", len(d.Changes))
	}
	for _, ch := range d.Changes {
		switch ch.Path() {
		case "plain.go":
			if !ch.IsRenamed || len(ch.Tags) != 0 {
				t.Fatalf("rename classification: %+v", ch)
			}
		case "fixtures/remove.txt":
			if !ch.IsDeleted || !reflect.DeepEqual(ch.Tags, []cg.Tag{cg.TestFixtureTag}) {
				t.Fatalf("delete classification: %+v", ch)
			}
		case "fixtures/binary.bin":
			if !ch.IsNew || !ch.IsBinary || !slices.Contains(ch.Tags, cg.TestFixtureTag) {
				t.Fatalf("binary classification: %+v", ch)
			}
		case "dist/large.min.js":
			if !ch.NewContentMissing || !reflect.DeepEqual(ch.Tags, []cg.Tag{cg.BuildOutputTag, cg.MinifiedTag}) {
				t.Fatal("opaque path lost tags")
			}
		case "kitex_gen/new.go":
			if !reflect.DeepEqual(ch.Tags, []cg.Tag{cg.GeneratedTag}) {
				t.Fatal(ch.Tags)
			}
		default:
			t.Fatal(ch.Path())
		}
	}
	selected := d.Select("kitex_gen/new.go")
	// Formation consumes the captured tags even if the live path disappears.
	if err := os.Remove(filepath.Join(root, "kitex_gen/new.go")); err != nil {
		t.Fatal(err)
	}
	units, err := repocli.FormUnits(context.Background(), selected, repocli.UnitOptions{})
	if err != nil || len(units.Units) == 0 || len(units.Changes) != 1 || !slices.Contains(units.Changes[0].Tags, cg.GeneratedTag) {
		t.Fatal(units, err)
	}
}

func TestDiffCustomTagsAndValidation(t *testing.T) {
	root := fixture(t)
	put(t, root, "custom.pb.go", "broken source remains a pure diff")
	for _, rules := range [][]cg.TagRule{{}, {{Name: "custom", Pattern: `\.go$`}, {Name: "custom", Pattern: `custom`}}} {
		d, err := repocli.Diff(context.Background(), repocli.DiffRequest{Repository: root, TagRules: rules})
		if err != nil || !d.Complete || len(d.Changes) != 1 {
			t.Fatal(d, err)
		}
		tags := d.Changes[0].Tags
		if len(rules) == 0 && len(tags) != 0 || len(rules) > 0 && !reflect.DeepEqual(tags, []cg.Tag{"custom"}) {
			t.Fatal(tags)
		}
	}
	for _, rule := range []cg.TagRule{{Name: "custom", Pattern: "["}, {Name: " ", Pattern: ".*"}} {
		_, err := repocli.Diff(context.Background(), repocli.DiffRequest{Repository: filepath.Join(root, "absent"), TagRules: []cg.TagRule{rule}})
		if err == nil || !strings.Contains(err.Error(), "tag rule") {
			t.Fatal("rule must fail before repository I/O", err)
		}
	}
}

func TestGraphPreservesDirectoryAndTags(t *testing.T) {
	root := fixture(t)
	put(t, root, "kitex_gen/service.pb.go", "package service\nfunc Work(){}\n")
	g, err := repocli.Graph(context.Background(), repocli.InputRequest{Repository: root}, 100)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]cg.Node{}
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}
	doc := byID[cg.DocumentID("kitex_gen/service.pb.go")]
	dir := byID[cg.DirectoryID("kitex_gen")]
	if !slices.Contains(doc.Tags, cg.GeneratedTag) || !slices.Contains(dir.Tags, cg.GeneratedTag) || dir.Location != nil || dir.Path != "kitex_gen" {
		t.Fatal(doc, dir)
	}
	manifest := byID[cg.DocumentID("go.mod")]
	if !slices.Contains(manifest.Tags, cg.ManifestTag) || manifest.Manifest == nil {
		t.Fatal(manifest)
	}
	found := false
	for _, r := range g.Relations {
		found = found || r.Kind == cg.InDirectory && r.Source == doc.ID && r.Target == dir.ID
	}
	if !found {
		t.Fatal("missing directory relationship")
	}
	if _, ok := g.Sources[dir.Path]; ok {
		t.Fatal("directory invented source")
	}
}

func TestDirectoryDoesNotMergeIndependentUnits(t *testing.T) {
	root := fixture(t)
	put(t, root, "src/a.ts", "export function Alpha(){}\n")
	put(t, root, "src/b.ts", "export function Beta(){}\n")
	d, err := repocli.Diff(context.Background(), repocli.DiffRequest{Repository: root})
	if err != nil {
		t.Fatal(err)
	}
	r, err := repocli.FormUnits(context.Background(), d, repocli.UnitOptions{MaxUnits: 1})
	if err != nil || len(r.Units) != 2 || !r.LimitExceeded {
		t.Fatal("physical directory merged independent modules", r, err)
	}
}

func TestFormUnitsExcludesChangesBeforeSplitting(t *testing.T) {
	root := fixture(t)
	put(t, root, "normal.go", "package main\nfunc Work() {}\n")
	put(t, root, "broken.pb.go", "not valid Go source")
	d, err := repocli.Diff(context.Background(), repocli.DiffRequest{Repository: root})
	if err != nil || len(d.Changes) != 2 {
		t.Fatal(d, err)
	}
	calls := 0
	r, err := repocli.FormUnits(context.Background(), d, repocli.UnitOptions{
		ExcludeChange: func(ch repocli.Change) bool {
			calls++
			return slices.Contains(ch.Tags, cg.GeneratedTag)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(r.Changes) != 1 || r.Changes[0].Path() != "normal.go" || len(r.Units) == 0 {
		t.Fatalf("calls=%d report=%+v", calls, r)
	}
	for _, f := range r.Fragments {
		if f.Path != "normal.go" {
			t.Fatalf("excluded fragment: %+v", f)
		}
	}
	for _, diagnostic := range r.Diagnostics {
		if diagnostic.Path == "broken.pb.go" {
			t.Fatalf("excluded file was analyzed: %+v", diagnostic)
		}
	}
	if len(d.Changes) != 2 {
		t.Fatal("mutated original diff")
	}
	if content, err := d.ReadSource(false, "broken.pb.go"); err != nil || content != "not valid Go source" {
		t.Fatal("lost captured context", content, err)
	}
	empty, err := repocli.FormUnits(context.Background(), d, repocli.UnitOptions{ExcludeChange: func(repocli.Change) bool { return true }})
	if err != nil || len(empty.Changes) != 0 || len(empty.Fragments) != 0 || len(empty.Units) != 0 {
		t.Fatal("empty selection restored excluded files", empty, err)
	}
}
