package units

import (
	"context"
	"encoding/json"
	"fmt"
	cg "github.com/compforge/codegraph"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func graph(t *testing.T, id string, files map[string]string) *cg.Graph {
	t.Helper()
	var docs []cg.Document
	for p, s := range files {
		docs = append(docs, cg.Document{Path: p, Content: []byte(s)})
	}
	g, _, err := cg.Build(context.Background(), id, docs, cg.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return g
}
func added(path, source string) Change {
	lines := strings.Split(strings.TrimSuffix(source, "\n"), "\n")
	return Change{NewPath: path, IsNew: true, NewFileContent: source, Diff: fmt.Sprintf("@@ -0,0 +1,%d @@\n+%s\n", len(lines), strings.Join(lines, "\n+")), Insertions: int64(len(lines))}
}

func TestSharedImportDoesNotMergeUsers(t *testing.T) {
	for _, tc := range []struct{ path, source string }{
		{"app.go", "package p\nimport \"fmt\"\nfunc A(){fmt.Println(1)}\nfunc B(){fmt.Println(2)}\n"},
		{"app.py", "import json\ndef a():\n    return json.dumps(1)\ndef b():\n    return json.dumps(2)\n"},
		{"app.ts", "import {parse} from './lib';\nexport function a(){return parse('1')}\nexport function b(){return parse('2')}\n"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			g := graph(t, "after", map[string]string{tc.path: tc.source, "lib.ts": "export function parse(s:string){return s}"})
			result, err := Form(context.Background(), Input{Changes: []Change{added(tc.path, tc.source)}, After: g})
			if err != nil {
				t.Fatal(err)
			}
			var imports []string
			for _, f := range result.Fragments {
				if f.Counts().Only(Import) {
					imports = append(imports, f.ID)
				}
			}
			if len(imports) != 1 {
				t.Fatalf("imports=%v", imports)
			}
			users := 0
			for _, u := range result.Units {
				if slices.Contains(u.FragmentIDs, imports[0]) {
					users++
				}
			}
			want := 2
			if tc.path == "app.go" {
				want = 1
			} // Current CodeGraph has no qualified Go import binding.
			if users != want {
				t.Fatalf("want two independent users of shared import, got %d; %+v edges=%+v", users, result.Units, result.Relations)
			}
			if err := ValidateEdits(added(tc.path, tc.source), result.Fragments); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestUnboundAndUnattachedImportsCoalesceByKind(t *testing.T) {
	source := "package p\nimport (\n _ \"one\"\n _ \"two\"\n)\n\nfunc A(){}\n"
	r, err := Form(context.Background(), Input{Changes: []Change{added("a.go", source)}, After: graph(t, "new", map[string]string{"a.go": source})})
	if err != nil {
		t.Fatal(err)
	}
	imports, unknown := 0, 0
	for _, u := range r.Units {
		kinds := map[Kind]bool{}
		for _, id := range u.FragmentIDs {
			for _, f := range r.Fragments {
				if f.ID == id {
					for kind := range f.Counts().After {
						kinds[kind] = true
					}
				}
			}
		}
		if kinds["import"] {
			imports++
			if kinds["function"] || kinds["unknown"] {
				t.Fatal("unattached import guessed a user", u)
			}
		}
		if kinds["unknown"] {
			unknown++
		}
	}
	if imports != 1 || unknown != 1 {
		t.Fatalf("coalescing: %+v", r.Units)
	}
}

func TestCountsPreserveReplacementDeletionAndSharedElements(t *testing.T) {
	f := Fragment{Before: []Element{{Kind: Class}}, After: []Element{{Kind: Interface}}}
	c := f.Counts()
	if c.Before[Class] != 1 || c.After[Interface] != 1 || c.After[Class] != 0 || c.Only(Import) {
		t.Fatal(c)
	}
	f.After = nil
	if !f.Counts().Only(Class) {
		t.Fatal(f.Counts())
	}
	f = Fragment{Diff: "@@ -0,0 +1 @@\n+do_unknown()\n"}
	if !f.Counts().Only(Unknown) {
		t.Fatal(f.Counts())
	}
	b, err := json.Marshal(f)
	if err != nil || !strings.Contains(string(b), `"counts":{"before":{},"after":{"unknown":1}}`) {
		t.Fatal(string(b), err)
	}
	imp := Fragment{Path: "app.py", After: []Element{{Path: "app.py", Kind: Import, Span: Span{1, 1}}}, Diff: "@@ -0,0 +1 @@\n+import json\n"}
	counts := countElements([]Fragment{imp, imp})
	if counts.After[Import] != 1 || !counts.Only(Import) {
		t.Fatal(counts)
	}
	if (ElementCounts{}).Only(Import) {
		t.Fatal("empty counts classified as import-only")
	}
}

func TestCrossFileMergeDeduplicatesSharedFragmentAndHonorsBudget(t *testing.T) {
	files := map[string]string{"go.mod": "module example\n", "a.go": "package p\nimport \"fmt\"\nfunc A(){fmt.Println(1); C()}\nfunc B(){fmt.Println(2); C()}\n", "b.go": "package p\nfunc C(){}\n"}
	input := Input{Changes: []Change{added("a.go", files["a.go"]), added("b.go", files["b.go"])}, After: graph(t, "after", files)}
	r, err := Form(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	cross := 0
	for _, u := range r.Units {
		if len(u.Paths) > 1 {
			cross++
			seen := map[string]bool{}
			for _, id := range u.FragmentIDs {
				if seen[id] {
					t.Fatal("duplicate shared fragment", id)
				}
				seen[id] = true
			}
		}
	}
	if cross != 1 {
		t.Fatalf("cross groups %+v", r.Units)
	}
	input.Options.MaxDiffSize = 1
	r, err = Form(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range r.Units {
		if len(u.Paths) > 1 {
			t.Fatal("crossed budget", u)
		}
	}
	blocked := false
	for _, s := range r.Steps {
		blocked = blocked || s.BudgetBlocked > 0
	}
	if !blocked {
		t.Fatal("missing cut reason")
	}
	input.Options = Options{}
	slices.Reverse(input.Changes)
	again, err := Form(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(input.Changes)
	original, _ := Form(context.Background(), input)
	if !reflect.DeepEqual(again, original) {
		t.Fatal("input order changed formation")
	}
}

func TestOldSideRenameAndCancellation(t *testing.T) {
	old := "package p\nfunc A(){}\n"
	next := "package p\nfunc A(){println(1)}\n"
	ch := Change{OldPath: "old.go", NewPath: "new.go", OldContentKnown: true, OldFileContent: old, NewFileContent: next, BeforeRef: "old", AfterRef: "new", IsRenamed: true, Diff: "@@ -2 +2 @@\n-func A(){}\n+func A(){println(1)}\n", Insertions: 1, Deletions: 1}
	fs, err := Split(context.Background(), ch)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 || fs[0].Before[0].Path != "old.go" || fs[0].After[0].Path != "new.go" || fs[0].BeforeRef == fs[0].AfterRef {
		t.Fatal(fs)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Form(ctx, Input{}); err == nil {
		t.Fatal("ignored cancellation")
	}
}
