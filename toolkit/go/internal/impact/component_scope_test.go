package impact

import (
	"context"
	"slices"
	"testing"

	"github.com/compforge/repocli/toolkit/go/internal/diff"
	"github.com/compforge/repocli/toolkit/go/internal/project"
)

// +case:id=component_graph_scope,desc=`Diff owners bound graph parsing, including nested Components and imports`,expect=`Only selected Component sources enter either graph`
func TestWorksetsOnlyBuildDiffComponents(t *testing.T) {
	snapshot := files(map[string]string{
		"package.json":          "{}",
		"root.ts":               "import './a/a'; export const root = 1;",
		"root.test.ts":          "import './root';",
		"a/package.json":        "{}",
		"a/a.ts":                "import '../b/b'; export const a = 1;",
		"a/a.test.ts":           "import './a';",
		"a/nested/package.json": "{}",
		"a/nested/n.ts":         "import '../a';",
		"b/package.json":        "{}",
		"b/b.ts":                "import '../a/a'; export const b = 1;",
		"b/b.test.ts":           "import './b';",
	})
	layout, err := project.Load(snapshot, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name        string
		paths, want []string
	}{
		{"one", []string{"a/a.ts"}, []string{"a/a.test.ts", "a/a.ts"}},
		{"two", []string{"a/a.ts", "b/b.ts"}, []string{"a/a.test.ts", "a/a.ts", "b/b.test.ts", "b/b.ts"}},
		{"root", []string{"root.ts"}, []string{"root.test.ts", "root.ts"}},
		{"nested", []string{"a/nested/n.ts"}, []string{"a/nested/n.ts"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var changes []diff.Change
			for _, name := range tc.paths {
				changes = append(changes, diff.Change{Path: name, Status: "modified"})
			}
			req := Request{Before: snapshot, After: snapshot, OldLayout: layout, NewLayout: layout, Changes: changes, MaxFiles: len(tc.want)}
			builds, err := buildWorksets(context.Background(), req, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, built := range builds {
				if !slices.Equal(built.ParsedFiles, tc.want) {
					t.Fatalf("parsed %v, want %v", built.ParsedFiles, tc.want)
				}
				for _, d := range built.Diagnostics {
					if d.Code == "file_limit" {
						t.Fatalf("unselected Component consumed budget: %+v", d)
					}
				}
			}
			// Default test discovery stays within selected Components and shared tests.
			result, err := Analyze(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range result.AffectedFiles {
				if !slices.Contains(tc.want, f.Path) {
					t.Fatalf("outside Component scope: %+v", f)
				}
			}
			for _, name := range result.TestFiles {
				if !slices.Contains(tc.want, name) {
					t.Fatalf("outside test scope: %s", name)
				}
			}
		})
	}
}

func TestComponentScopeRetainsBothSidesOfMovesAndBoundaryChanges(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		before, after         map[string]string
		change                diff.Change
		wantBefore, wantAfter []string
	}{
		{"move", map[string]string{"a/package.json": "{}", "a/old.ts": "export const a=1;", "b/package.json": "{}", "b/use.ts": "import '../a/old';"},
			map[string]string{"a/package.json": "{}", "a/use.ts": "import '../b/new';", "b/package.json": "{}", "b/new.ts": "export const a=1;"},
			diff.Change{Path: "b/new.ts", OldPath: "a/old.ts", Status: "renamed"}, []string{"a/old.ts", "b/use.ts"}, []string{"a/use.ts", "b/new.ts"}},
		{"delete", map[string]string{"a/package.json": "{}", "a/old.ts": "export const a=1;"},
			map[string]string{"a/package.json": "{}", "a/use.ts": "import './old';"},
			diff.Change{Path: "a/old.ts", Status: "deleted"}, []string{"a/old.ts"}, []string{"a/use.ts"}},
		{"new boundary", map[string]string{"a/code.ts": "export const a=1;"},
			map[string]string{"a/package.json": "{}", "a/code.ts": "export const a=1;"},
			diff.Change{Path: "a/package.json", Status: "added"}, nil, []string{"a/code.ts"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, after := files(tc.before), files(tc.after)
			oldLayout, _ := project.Load(before, "")
			newLayout, _ := project.Load(after, "")
			req := Request{Before: before, After: after, OldLayout: oldLayout, NewLayout: newLayout, Changes: []diff.Change{tc.change}}
			builds, err := buildWorksets(context.Background(), req, nil)
			if err != nil {
				t.Fatal(err)
			}
			for side, want := range [][]string{tc.wantBefore, tc.wantAfter} {
				if !slices.Equal(builds[side].ParsedFiles, want) {
					t.Fatalf("side %d: parsed %v, want %v", side, builds[side].ParsedFiles, want)
				}
			}
		})
	}
}

func TestUnownedChangeDoesNotSelectSiblingComponents(t *testing.T) {
	result := scoped(t, map[string]string{
		"shared.ts": "export const a=1;", "shared.test.ts": "import './shared';",
		"app/package.json": "{}", "app/a.test.ts": "import '../shared';",
	}, "shared.ts")
	if !slices.Equal(result.TestFiles, []string{"shared.test.ts"}) {
		t.Fatal(result)
	}
}
