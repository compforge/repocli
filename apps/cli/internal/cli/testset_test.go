package cli

import (
	"reflect"
	"testing"
)

func TestImpactEmptyTestsetDoesNotDegradeOtherComponents(t *testing.T) {
	dir := t.TempDir()
	gitCommand(t, dir, "init", "-q", "-b", "main")
	put(t, dir, "lib/package.json", "{}")
	put(t, dir, "lib/value.ts", "export const value=1;\n")
	put(t, dir, "app/package.json", "{}")
	put(t, dir, "app/value.test.ts", "import {value} from '../lib/value';\n")
	gitCommand(t, dir, "add", ".")
	gitCommand(t, dir, "commit", "-qm", "fixture")
	put(t, dir, "lib/value.ts", "export const value=2;\n")
	for _, extra := range [][]string{nil, {"--test-dir", "lib"}, {"--test-dir", "missing"}} {
		r := runJSON(t, append([]string{"impact", "--repo", dir, "--json"}, extra...), "")
		if !r.Complete || r.Scope != "focused" || len(r.TestFiles) != 0 || len(r.Diagnostics) != 0 {
			t.Fatal(r)
		}
		for _, c := range r.Components {
			if !c.Complete || c.Affected != (c.Root == "lib") {
				t.Fatal(c)
			}
		}
	}
	// Explicit test discovery is independent from lib's production scope.
	r := runJSON(t, []string{"impact", "--repo", dir, "--test-dir", ".", "--json"}, "")
	if !r.Complete || !reflect.DeepEqual(r.TestFiles, []string{"app/value.test.ts"}) {
		t.Fatal(r)
	}
	for _, c := range r.Components {
		if c.Root == "app" && (!reflect.DeepEqual(c.TestFiles, r.TestFiles) || len(c.ChangedFiles) != 0) {
			t.Fatal(c)
		}
	}
}

func TestRepositoryTestsDoNotSelectRootProductionSources(t *testing.T) {
	dir := t.TempDir()
	gitCommand(t, dir, "init", "-q", "-b", "main")
	for name, content := range map[string]string{
		"package.json": "{}", "unrelated.ts": "invalid (((",
		"lib/package.json": "{}", "lib/value.ts": "export const value=1;\n",
		"tests/value.test.ts": "import {value} from './helper';", "tests/helper.ts": "export {value} from '../lib/value';",
	} {
		put(t, dir, name, content)
	}
	gitCommand(t, dir, "add", ".")
	gitCommand(t, dir, "commit", "-qm", "fixture")
	put(t, dir, "lib/value.ts", "export const value=2;\n")
	r := runJSON(t, []string{"impact", "--repo", dir, "--json"}, "")
	if !r.Complete || !reflect.DeepEqual(r.TestFiles, []string{"tests/value.test.ts"}) || len(r.Observations) != 0 {
		t.Fatal(r)
	}
	for _, c := range r.Components {
		if c.Root == "." && (!reflect.DeepEqual(c.TestFiles, r.TestFiles) || len(c.ChangedFiles) != 0) {
			t.Fatal(c)
		}
	}
}
