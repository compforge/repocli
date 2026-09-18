package impact

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/compforge/repocli/internal/diff"
)

func files(values map[string]string) map[string][]byte {
	out := map[string][]byte{}
	for name, data := range values {
		out[name] = []byte(data)
	}
	return out
}

func runChange(t *testing.T, base map[string]string, name, old, new string) Result {
	t.Helper()
	after := map[string]string{}
	for k, v := range base {
		after[k] = v
	}
	after[name] = strings.Replace(base[name], old, new, 1)
	line := strings.Count(strings.Split(base[name], old)[0], "\n") + 1
	r, err := Analyze(context.Background(), Request{Before: files(base), After: files(after), TestDirs: []string{"tests"},
		Changes: []diff.Change{{Path: name, Status: "modified", Hunks: []diff.Hunk{{Old: diff.Range{Start: line, Count: 1}, New: diff.Range{Start: line, Count: 1}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestNamedImportsSelectChangedSymbolWithoutCheckingUse(t *testing.T) {
	base := map[string]string{
		"src/math.ts":          "export function a() { return 1; }\nexport function b() { return 2; }\n",
		"tests/a.test.ts":      "import { a as unusedAlias } from '../src/math';\n",
		"tests/b.test.ts":      "import { b } from '../src/math';\n",
		"tests/module.test.ts": "import * as math from '../src/math';\n",
	}
	r := runChange(t, base, "src/math.ts", "return 1", "return 3")
	if r.Scope != "focused" || !reflect.DeepEqual(r.TestFiles, []string{"tests/a.test.ts", "tests/module.test.ts"}) {
		t.Fatalf("result: %+v", r)
	}
	if !reflect.DeepEqual(r.SourceFiles, []string{"src/math.ts"}) {
		t.Fatalf("source: %v", r.SourceFiles)
	}
	if len(r.Changes[0].After) != 1 || r.Changes[0].After[0].Name != "a" {
		t.Fatalf("symbols: %+v", r.Changes[0])
	}
}

func TestPythonNamedImports(t *testing.T) {
	base := map[string]string{
		"src/mathlib.py":       "def a():\n    return 1\n\ndef b():\n    return 2\n",
		"tests/test_a.py":      "from ..src.mathlib import a as unused\n",
		"tests/test_b.py":      "from ..src.mathlib import b\n",
		"tests/test_module.py": "from ..src import mathlib\n",
	}
	r := runChange(t, base, "src/mathlib.py", "return 1", "return 3")
	if r.Scope != "focused" || !reflect.DeepEqual(r.TestFiles, []string{"tests/test_a.py", "tests/test_module.py"}) {
		t.Fatalf("result: %+v", r)
	}
}

func TestTransitiveImportsAndExportAliases(t *testing.T) {
	base := map[string]string{
		"src/a.ts":        "function local() { return 1; }\nexport { local as publicName };\n",
		"src/barrel.ts":   "export { publicName } from './a';\n",
		"tests/a.test.ts": "import { publicName } from '../src/barrel';\n",
		"tests/b.test.ts": "export const unrelated = 1;\n",
	}
	r := runChange(t, base, "src/a.ts", "return 1", "return 2")
	if r.Scope != "focused" || !reflect.DeepEqual(r.TestFiles, []string{"tests/a.test.ts"}) {
		t.Fatalf("result: %+v", r)
	}
	if len(r.Reasons[0].DependencyPath) < 3 {
		t.Fatalf("missing transitive explanation: %+v", r.Reasons)
	}
}

func TestGoPackageAndTransitiveImports(t *testing.T) {
	base := map[string]string{
		"go.mod":                "module example.com/sample\n\ngo 1.25.0\n",
		"core/a.go":             "package core\nfunc A() int { return 1 }\n",
		"service/service.go":    "package service\nimport \"example.com/sample/core\"\nfunc Run() int { return core.A() }\n",
		"tests/service_test.go": "package tests\nimport \"example.com/sample/service\"\nvar _ = service.Run\n",
		"tests/other_test.go":   "package tests\nfunc helper() {}\n",
	}
	r := runChange(t, base, "core/a.go", "return 1", "return 2")
	if r.Scope != "focused" || !reflect.DeepEqual(r.TestFiles, []string{"tests/other_test.go", "tests/service_test.go"}) {
		t.Fatalf("result: %+v", r)
	}
}

func TestUncertainAssociationsAreNotOutput(t *testing.T) {
	for _, tc := range []struct{ name, file, source string }{
		{"dynamic", "src/loader.ts", "export const load = () => import(target);\n"},
		{"unresolved", "src/loader.ts", "import { a } from './missing';\nexport const load = 1;\n"},
		{"configuration", "package.json", "{}\n"},
		{"fixture", "tests/conftest.py", "def helper():\n    return 1\n"},
		{"unsupported", "data.json", "{}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := map[string][]byte{tc.file: []byte(tc.source), "tests/test_a.py": []byte("def test_a():\n    pass\n")}
			r, err := Analyze(context.Background(), Request{Before: before, After: before, Changes: []diff.Change{{Path: tc.file, Status: "modified"}}, TestDirs: []string{"tests"}})
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "dynamic" || tc.name == "unresolved" {
				// Outgoing uncertainty on an unreferenced changed source cannot
				// create an incoming import from this independent candidate.
				if r.Scope != "focused" || len(r.TestFiles) != 0 {
					t.Fatalf("%+v", r)
				}
				return
			}
			if r.Scope != "partial" || len(r.FallbackReasons) == 0 || len(r.TestFiles) != 0 {
				t.Fatalf("result: %+v", r)
			}
		})
	}
}

func TestDeletedTestsNeverSelectedAndSourceUsesNewPath(t *testing.T) {
	before := files(map[string]string{"tests/old_test.py": "def test_old():\n    pass\n", "old.py": "a = 1\n"})
	after := files(map[string]string{"tests/new_test.py": "def test_new():\n    pass\n", "new.py": "a = 1\n"})
	r, err := Analyze(context.Background(), Request{Before: before, After: after, TestDirs: []string{"tests"}, Changes: []diff.Change{
		{Path: "tests/old_test.py", Status: "deleted"}, {Path: "new.py", OldPath: "old.py", Status: "renamed"}, {Path: "tests/new_test.py", Status: "added"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.TestFiles, []string{"tests/new_test.py"}) {
		t.Fatalf("tests: %v", r.TestFiles)
	}
	if !reflect.DeepEqual(r.SourceFiles, []string{"new.py", "tests/new_test.py", "tests/old_test.py"}) {
		t.Fatalf("source: %v", r.SourceFiles)
	}
}

func TestModuleLevelEditBroadensAndOldImportsRemain(t *testing.T) {
	base := map[string]string{"src/a.ts": "import './setup';\nexport function a() { return 1; }\nexport function b() { return 2; }\n",
		"src/setup.ts": "export const setup = 1;\n", "tests/a.test.ts": "import { b } from '../src/a';\n"}
	r := runChange(t, base, "src/a.ts", "import './setup';", "// removed import")
	if r.Scope != "focused" || len(r.TestFiles) != 1 {
		t.Fatalf("result: %+v", r)
	}
}

func TestNoChangesAndNoTestRequest(t *testing.T) {
	r, err := Analyze(context.Background(), Request{TestDirs: []string{"tests"}})
	if err != nil || r.Scope != "focused" || len(r.TestFiles) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = Analyze(context.Background(), Request{After: files(map[string]string{"a.ts": "export const a = 1;\n"}), Changes: []diff.Change{{Path: "a.ts", Status: "added"}}})
	if err != nil || r.Scope != "not_requested" || len(r.SourceFiles) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestImportedTestHelperUsesDependencyGraph(t *testing.T) {
	base := map[string]string{
		"tests/helper.py": "def a():\n    return 1\n\ndef b():\n    return 2\n",
		"tests/test_a.py": "from .helper import a\n",
		"tests/test_b.py": "from .helper import b\n",
	}
	r := runChange(t, base, "tests/helper.py", "return 1", "return 3")
	if r.Scope != "focused" || !reflect.DeepEqual(r.TestFiles, []string{"tests/test_a.py"}) {
		t.Fatalf("helper treated as an implicit configuration hook: %+v", r)
	}
}

func TestValidateDirsRejectsAbsoluteAndEmptyRoots(t *testing.T) {
	for _, dir := range []string{"/", "/tmp", "", "../tests"} {
		if _, err := ValidateDirs([]string{dir}); err == nil {
			t.Errorf("accepted invalid root %q", dir)
		}
	}
	for _, dir := range []string{".", "./"} {
		got, err := ValidateDirs([]string{dir})
		if err != nil || !reflect.DeepEqual(got, []string{"."}) {
			t.Fatalf("root %q: %v %v", dir, got, err)
		}
	}
}
