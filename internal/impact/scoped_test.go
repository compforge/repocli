package impact

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/compforge/repocli/internal/diff"
	"github.com/compforge/repocli/internal/project"
)

func scoped(t *testing.T, content map[string]string, changed string, dirs ...string) Result {
	t.Helper()
	snapshot := files(content)
	layout, err := project.Load(snapshot, "")
	if err != nil {
		t.Fatal(err)
	}
	r, err := Analyze(context.Background(), Request{Before: snapshot, After: snapshot, Changes: []diff.Change{{Path: changed, Status: "modified"}}, TestDirs: dirs, Mode: "file", OldLayout: layout, NewLayout: layout})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestComponentGapsDoNotInventTestAssociations(t *testing.T) {
	content := map[string]string{
		"api/package.json": "{}", "api/src/a.ts": "export const a=1;",
		"api/a.test.ts":       "import {a} from './src/a';",
		"client/package.json": "{}", "client/a.test.ts": "import {a} from '../api/src/a';",
		"other/package.json": "{}", "other/a.test.ts": "export const unrelated=1;",
	}
	r := scoped(t, content, "api/package.json", ".")
	if r.Scope != "partial" || len(r.TestFiles) != 0 {
		t.Fatalf("%+v", r)
	}
	if len(r.Uncertainties) != 1 || r.Uncertainties[0].Scope != "component" {
		t.Fatal(r.Uncertainties)
	}
	// Root resources cannot borrow a sibling's ownership boundary.
	content["shared.json"] = "{}"
	r = scoped(t, content, "shared.json", ".")
	if len(r.TestFiles) != 0 || r.Uncertainties[0].Scope != "repository" {
		t.Fatal(r)
	}
}

func TestUnvisitedSourceIsNotParsed(t *testing.T) {
	content := map[string]string{
		"api/package.json": "{}", "api/a.ts": "export const a=1;", "api/a.test.ts": "import {a} from './a';",
		"tools/package.json": "{}", "tools/loader.ts": "export const load=()=>import(target);",
	}
	r := scoped(t, content, "api/a.ts", ".")
	if r.Scope != "focused" || len(r.TestFiles) != 1 || len(r.Observations) != 0 || len(r.FallbackReasons) != 0 {
		t.Fatal(r)
	}
	// A requested test with unknown imports makes the analysis partial.
	// That unknown relationship must not add it to the output.
	content["tools/a.test.ts"] = "import {load} from './loader';"
	r = scoped(t, content, "api/a.ts", ".")
	if r.Scope != "partial" || len(r.TestFiles) != 1 || len(r.Observations) != 0 {
		t.Fatal(r)
	}
}

func TestLocalTSConfigInheritance(t *testing.T) {
	content := map[string]string{
		"tsconfig.base.json": `{"compilerOptions":{"strict":true,"moduleResolution":"bundler"}}`,
		"tsconfig.json":      `{"extends":"./tsconfig.base"}`,
		"src/a.ts":           "export const a=1;", "tests/a.test.ts": "import {a} from '../src/a';",
	}
	r := scoped(t, content, "src/a.ts", "tests")
	if r.Scope != "focused" || len(r.TestFiles) != 1 {
		t.Fatal(r)
	}
	// Leave a candidate whose membership is not already proven; unresolved
	// compiler configuration can still change that answer.
	content["tests/other.test.ts"] = "export const other=1;"
	for _, parent := range []string{`{"extends":"./tsconfig.json"}`, `{"extends":"../outside"}`, `{"extends":"missing-package"}`, `{"extends":"./missing"}`, `{"compilerOptions":{"paths":{"@/*":["src/*"]}}}`} {
		content["tsconfig.base.json"] = parent
		if got := scoped(t, content, "src/a.ts", "tests"); got.Scope != "partial" {
			t.Fatalf("accepted %s: %+v", parent, got)
		}
	}
}

func TestPythonStaticImportsAndPaths(t *testing.T) {
	content := map[string]string{
		"pkg/mathlib.py":      "def a():\n    return 1\n",
		"tests/test_a.py":     "from pathlib import Path\nimport sys, importlib\nsys.path.insert(0,str(Path(__file__).parent.parent / 'pkg'))\nm = importlib.import_module('mathlib')\n",
		"tests/test_b.py":     "from pathlib import Path\nimport sys\nsys.path.insert(0,str(Path(__file__).parent.parent / 'pkg'))\nm = __import__('mathlib')\n",
		"scripts/entry.py":    "from pathlib import Path\nimport sys\nsys.path.insert(0, str(Path(__file__).resolve().parent.parent / 'pkg'))\nfrom mathlib import a\n",
		"tests/test_entry.py": "from ..scripts.entry import a\n",
	}
	r := scoped(t, content, "pkg/mathlib.py", "tests")
	if r.Scope != "focused" || len(r.TestFiles) != 3 || len(r.Observations) != 0 {
		t.Fatal(r)
	}
	for _, call := range []string{"importlib.import_module(target)", "__import__(target)", "sys.path.append('/external')", "sys.path.insert(0, str(Path(__file__).parents[10]))"} {
		content["scripts/entry.py"] = "from pathlib import Path\nimport sys, importlib\n" + call + "\nfrom mathlib import a\n"
		got := scoped(t, content, "pkg/mathlib.py", "tests")
		// The two explicit-path imports remain proven. The entry's bare import
		// has no definite search root, so it cannot select the third candidate.
		if got.Scope != "partial" || len(got.TestFiles) != 2 {
			t.Fatalf("%s: %+v", call, got)
		}
	}
}

func TestWorkspaceEntrypointsAndInheritedConfigEdges(t *testing.T) {
	content := map[string]string{
		"lib/package.json":  `{"name":"@demo/lib","exports":{".":"./src/index.ts","./helper":"./src/helper.ts"}}`,
		"lib/src/index.ts":  "export {a} from './helper';",
		"lib/src/helper.ts": "export const a=1;",
		"lib/src/cjs.ts":    "export const a=2;",
		"app/package.json":  "{}", "app/test.test.ts": "import {a} from '@demo/lib';",
		"app/direct.test.ts": "import {a} from '@demo/lib/helper';",
		"other/package.json": "{}", "other/test.test.ts": "export const test=1;",
	}
	got := scoped(t, content, "lib/src/helper.ts", ".")
	if got.Scope != "focused" || !reflect.DeepEqual(got.TestFiles, []string{"app/direct.test.ts", "app/test.test.ts"}) {
		t.Fatal(got)
	}
	got = scoped(t, content, "lib/src/cjs.ts", ".")
	if got.Scope != "focused" || len(got.TestFiles) != 0 {
		t.Fatal(got)
	}
	content["lib/tsconfig.base.json"] = `{"compilerOptions":{"strict":true}}`
	content["other/tsconfig.json"] = `{"extends":"../lib/tsconfig.base.json"}`
	got = scoped(t, content, "lib/tsconfig.base.json", ".")
	if got.Scope != "partial" || len(got.TestFiles) != 0 {
		t.Fatal(got)
	}
	affected := false
	for _, u := range got.Uncertainties {
		if slices.Contains(u.TestFiles, "other/test.test.ts") {
			affected = true
		}
	}
	if !affected {
		t.Fatal("inherited configuration gap was not attributed to its consumer")
	}
	for _, exports := range []string{`{".":{"import":"./src/index.ts","require":"./src/cjs.ts"}}`, `{".":"./dist/missing.js"}`, `{".":"../../outside.ts"}`, `{".":["./src/index.ts"]}`, `{"./*":"./src/*.ts"}`} {
		content["lib/package.json"] = `{"name":"@demo/lib","exports":` + exports + `}`
		got = scoped(t, content, "lib/src/helper.ts", ".")
		if got.Scope != "partial" || len(got.FallbackReasons) == 0 {
			t.Fatalf("%s: %+v", exports, got)
		}
	}
}

func TestAmbiguousImportsAreOmittedRatherThanGuessed(t *testing.T) {
	for _, content := range []map[string]string{
		{"src/a.ts": "export const a=1;", "src/a.js": "export const a=2;", "tests/a.test.ts": "import {a} from '../src/a';"},
		{"one/a.py": "def a(): pass\n", "two/a.py": "def a(): pass\n", "tests/test_a.py": "from a import a\n"},
	} {
		changed := "src/a.ts"
		if _, ok := content[changed]; !ok {
			changed = "one/a.py"
		}
		got := scoped(t, content, changed, "tests")
		if got.Scope != "partial" || len(got.TestFiles) != 0 || (!strings.Contains(strings.Join(got.FallbackReasons, " "), "ambiguous") && !strings.Contains(strings.Join(got.FallbackReasons, " "), "inferred")) {
			t.Fatal(got)
		}
	}
}
