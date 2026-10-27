package impact

import (
	"context"
	"slices"
	"testing"

	"github.com/compforge/repocli/toolkit/go/internal/diff"
	"github.com/compforge/repocli/toolkit/go/internal/project"
)

func TestAmbiguityIsQueryRelative(t *testing.T) {
	content := map[string]string{
		"changed.ts": "export const x=1;",
		"a.ts":       "export const x=1;", "a.js": "export const x=2;",
		"test.test.ts": "import {x} from './a';",
	}
	got := scoped(t, content, "changed.ts", ".")
	if got.Scope != "focused" || len(got.TestFiles) != 0 || len(got.Observations) != 0 {
		t.Fatalf("%+v", got)
	}
	content["a.ts"] = "import {x} from './changed';"
	got = scoped(t, content, "changed.ts", ".")
	if got.Scope != "focused" || !slices.Equal(got.TestFiles, []string{"test.test.ts"}) || got.Reasons[0].Relations[0].Confidence != "weak" {
		t.Fatalf("%+v", got)
	}
	content["a.ts"] = "export const x=()=>import(target);"
	got = scoped(t, content, "changed.ts", ".")
	if got.Scope != "focused" || len(got.TestFiles) != 0 {
		t.Fatalf("unknown downstream route: %+v", got)
	}
}

func TestMetadataClassification(t *testing.T) {
	for _, tc := range []struct{ name, before, after, scope string }{
		{"VERSION", "1", "2", "focused"},
		{"sdk/VERSION", "1", "2", "focused"},
		{"README.md", "old docs", "new docs", "focused"},
		{"docs/lifecycle.md", "old docs", "new docs", "focused"},
		{"sdk/docs/API.rst", "old docs", "new docs", "focused"},
		{"docs/nested/guide.MD", "old docs", "new docs", "focused"},
		{"docs/openapi.yaml", "old schema", "new schema", "partial"},
		{"docs/settings.json", `{}`, `{"enabled":true}`, "partial"},
		{"docs/example.py", "value=1", "value=2", "focused"},
		{"assets/prompt.md", "old prompt", "new prompt", "focused"},
		{"skills/SKILL.md", "old instruction", "new instruction", "focused"},
		{"package.json", `{"description":"old"}`, `{"description":"new"}`, "focused"},
		{"package.json", `{"version":"1"}`, `{"version":"2"}`, "partial"},
		{"package.json", `{}`, `{"exports":"./new.ts"}`, "partial"},
		{"package.json", `{}`, `{"unknown":true}`, "partial"},
		{"version.lock.json", `{}`, `{"v":2}`, "partial"},
	} {
		before := files(map[string]string{tc.name: tc.before, "src.ts": "export const x=1;", "test.test.ts": "import {x} from './src';"})
		after := files(map[string]string{tc.name: tc.after, "src.ts": "export const x=1;", "test.test.ts": "import {x} from './src';"})
		got, err := Analyze(context.Background(), Request{Before: before, After: after, Changes: []diff.Change{{Path: tc.name, Status: "modified"}}, TestDirs: []string{"."}})
		if err != nil {
			t.Fatal(err)
		}
		if got.Scope != tc.scope || len(got.TestFiles) != 0 {
			t.Fatalf("%s: %+v", tc.name, got)
		}
	}
}

func TestKnownMembershipAcrossVersionsIgnoresUnknownTargets(t *testing.T) {
	before := files(map[string]string{"src.ts": "export const x=1;", "test.test.ts": "import {x} from './src';"})
	after := files(map[string]string{"src.ts": "export const x=2;", "test.test.ts": "import(target);"})
	got, err := Analyze(context.Background(), Request{Before: before, After: after, Changes: []diff.Change{{Path: "src.ts", Status: "modified"}}, TestDirs: []string{"."}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Scope != "focused" || !slices.Equal(got.TestFiles, []string{"test.test.ts"}) || len(got.Observations) != 0 || got.Reasons[0].Version != "before" {
		t.Fatalf("%+v", got)
	}
}

func TestWorkspacePackageCandidatesRetainWeakRelations(t *testing.T) {
	snapshot := files(map[string]string{
		"one/package.json": `{"name":"demo","exports":"./entry.ts"}`,
		"two/package.json": `{"name":"demo","exports":"./entry.ts"}`,
		"one/entry.ts":     "export const value=1;", "two/entry.ts": "export const value=2;",
		"test.test.ts": "import {value} from 'demo';",
		"changed.ts":   "export const changed = true;",
	})
	layout, err := project.Load(snapshot, "")
	if err != nil {
		t.Fatal(err)
	}
	// Both package candidates and the unowned consumer are in diff scope.
	got, err := Analyze(context.Background(), Request{Before: snapshot, After: snapshot,
		OldLayout: layout, NewLayout: layout, TestDirs: []string{"."},
		Changes: []diff.Change{{Path: "one/entry.ts", Status: "modified"},
			{Path: "two/entry.ts", Status: "modified"}, {Path: "changed.ts", Status: "modified"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Scope != "focused" || !slices.Equal(got.TestFiles, []string{"test.test.ts"}) || got.Reasons[0].Relations[0].Confidence != "weak" {
		t.Fatal(got)
	}
}
