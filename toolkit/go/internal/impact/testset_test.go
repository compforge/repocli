package impact

import (
	"context"
	"slices"
	"testing"

	"github.com/compforge/repocli/toolkit/go/internal/diff"
	"github.com/compforge/repocli/toolkit/go/internal/project"
)

func testRequest(t *testing.T, content map[string]string, changed string, dirs ...string) Request {
	t.Helper()
	catalog := files(content)
	layout, err := project.Load(catalog, "")
	if err != nil {
		t.Fatal(err)
	}
	return Request{Before: catalog, After: catalog, OldLayout: layout, NewLayout: layout,
		Changes: []diff.Change{{Path: changed, Status: "modified"}}, TestDirs: dirs}
}

// +case:id=testset_locations,desc=`Language conventions and test directories are independent of production ownership`,expect=`Colocated, Component, shared and explicit tests remain selectable; empty testsets are complete`
func TestIndependentTestsetLocations(t *testing.T) {
	for _, tc := range []struct {
		name       string
		content    map[string]string
		changed    string
		dirs, want []string
	}{
		{"go colocated", map[string]string{"lib/go.mod": "module example.com/lib\n", "lib/lib.go": "package lib\nfunc Value() {}\n", "lib/lib_test.go": "package lib\nvar _ = Value\n"}, "lib/lib.go", nil, []string{"lib/lib_test.go"}},
		{"component tests", map[string]string{"lib/pyproject.toml": "[project]\nname='lib'\n", "lib/value.py": "value = 1\n", "lib/tests/test_value.py": "from lib.value import value\n"}, "lib/value.py", nil, []string{"lib/tests/test_value.py"}},
		{"repository tests", map[string]string{"package.json": "{}", "lib/package.json": "{}", "lib/value.ts": "export const value = 1;", "tests/value.test.ts": "import {value} from '../lib/value';"}, "lib/value.ts", nil, []string{"tests/value.test.ts"}},
		{"explicit directory", map[string]string{"package.json": "{}", "lib/package.json": "{}", "lib/value.ts": "export const value = 1;", "checks/value.test.ts": "import {value} from './helper';", "checks/helper.ts": "export {value} from '../lib/value';"}, "lib/value.ts", []string{"checks"}, []string{"checks/value.test.ts"}},
		{"explicit outside component", map[string]string{"lib/package.json": "{}", "lib/value.ts": "export const value = 1;", "client/package.json": "{}", "client/value.test.ts": "import {value} from '../lib/value';"}, "lib/value.ts", []string{"."}, []string{"client/value.test.ts"}},
		{"no tests", map[string]string{"lib/package.json": "{}", "lib/value.ts": "export const value = 1;"}, "lib/value.ts", nil, nil},
		{"empty requested directory", map[string]string{"lib/package.json": "{}", "lib/value.ts": "export const value = 1;", "client/package.json": "{}", "client/value.test.ts": "import {value} from '../lib/value';"}, "lib/value.ts", []string{"missing"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := testRequest(t, tc.content, tc.changed, tc.dirs...)
			got, err := Analyze(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if got.Scope != "focused" || len(got.FallbackReasons) != 0 || !slices.Equal(got.TestFiles, tc.want) {
				t.Fatalf("%+v", got)
			}
		})
	}
}

func TestWorksetRetainsContextAndOnlyExpandsAllowedSources(t *testing.T) {
	req := testRequest(t, map[string]string{
		"package.json": "{}", "tsconfig.base.json": `{"compilerOptions":{"strict":true}}`,
		"lib/package.json":    `{"name":"@example/lib","exports":"./value.ts"}`,
		"lib/tsconfig.json":   `{"extends":"../tsconfig.base.json"}`,
		"lib/value.ts":        "export const value = 1;",
		"tests/value.test.ts": "import {value} from './helper';",
		"tests/helper.ts":     "export {value} from '@example/lib'; import '../other/huge';",
		"tests/unused.ts":     "invalid (((",
		"other/package.json":  "{}", "other/huge.ts": "invalid (((",
		"other/huge.test.ts": "import './huge';",
	}, "lib/value.ts")
	req.MaxFiles = 3
	tests := discoverTests(req.After, req.NewLayout, selectComponents(req), req.TestDirs)
	builds, err := buildWorksets(context.Background(), req, tests)
	if err != nil {
		t.Fatal(err)
	}
	for _, built := range builds {
		if !slices.Equal(built.ParsedFiles, []string{"lib/value.ts", "tests/helper.ts", "tests/value.test.ts"}) {
			t.Fatal(built.ParsedFiles)
		}
		boundary := false
		for _, d := range built.Diagnostics {
			if d.Code != "source_scope" || d.Path != "other/huge.ts" {
				t.Fatalf("unexpected diagnostic: %+v", d)
			}
			boundary = true
		}
		if !boundary || len(built.Graph.Outgoing("lib/tsconfig.json")) == 0 {
			t.Fatal("lost source boundary or inherited configuration")
		}
	}
	got, err := Analyze(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Scope != "focused" || !slices.Equal(got.TestFiles, tests) {
		t.Fatal(got)
	}
	for _, f := range got.AffectedFiles {
		if f.Path == "other/huge.ts" {
			t.Fatal("unselected production source became affected")
		}
	}
}

func TestSelectedComponentsShareOneDependencyGraph(t *testing.T) {
	req := testRequest(t, map[string]string{
		"a/package.json": "{}", "a/value.ts": "export const value=1;",
		"b/package.json": "{}", "b/bridge.ts": "export {value} from '../a/value';", "b/changed.ts": "export const changed=1;",
		"tests/shared.test.ts": "import {value} from '../b/bridge';",
	}, "a/value.ts")
	req.Changes = append(req.Changes, diff.Change{Path: "b/changed.ts", Status: "modified"})
	got, err := Analyze(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.TestFiles, []string{"tests/shared.test.ts"}) {
		t.Fatal(got)
	}
	found := false
	for _, reason := range got.Reasons {
		if slices.Contains(reason.DependencyPath, "b/bridge.ts") {
			found = true
		}
	}
	if !found {
		t.Fatal("shared graph lost the intermediate Component")
	}
	// Removing b's diff keeps its reference, but does not traverse its production sources.
	req.Changes = req.Changes[:1]
	got, err = Analyze(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.TestFiles) != 0 || len(got.Observations) == 0 {
		t.Fatal(got)
	}
}

func TestTestOnlyDiffDoesNotSelectTestedComponent(t *testing.T) {
	req := testRequest(t, map[string]string{"package.json": "{}", "tests/value.test.ts": "import '../lib/value';", "lib/package.json": "{}", "lib/value.ts": "export const value=1;"}, "tests/value.test.ts")
	tests := discoverTests(req.After, req.NewLayout, selectComponents(req), nil)
	builds, err := buildWorksets(context.Background(), req, tests)
	if err != nil {
		t.Fatal(err)
	}
	for _, built := range builds {
		if !slices.Equal(built.ParsedFiles, tests) {
			t.Fatal(built.ParsedFiles)
		}
	}
}
