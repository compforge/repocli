package cli

import (
	"strings"
	"testing"
)

func TestConfigReferencesAcrossGitlinksRemainMissing(t *testing.T) {
	parent, child := submoduleFixture(t)
	committed := `{"compilerOptions":{"strict":true}}`
	put(t, child, "tsconfig.json", committed)
	gitCommand(t, child, "add", "tsconfig.json")
	gitCommand(t, child, "commit", "-qm", "config")
	put(t, parent, "tsconfig.json", `{"extends":"./child/tsconfig"}`)
	put(t, parent, "tests/other.test.ts", "export const other=1;\n")
	gitCommand(t, parent, "add", "child", "tsconfig.json", "tests/other.test.ts")
	gitCommand(t, parent, "commit", "-qm", "config consumer")
	put(t, child, "tsconfig.json", `{"compilerOptions":{"paths":{"alias":["./target"]}}}`)
	put(t, parent, "source file.ts", "export function a(){return 8;}\n")
	gitCommand(t, parent, "add", "source file.ts")
	staged := runJSON(t, []string{"diff", "--repo", parent, "--staged", "--test-dir", "tests", "--json"}, "")
	if staged.Complete {
		t.Fatalf("index config came from dirty child: %+v", staged)
	}
	working := runJSON(t, []string{"diff", "--repo", parent, "--test-dir", "tests", "--json"}, "")
	if working.Complete {
		t.Fatal("changed unsupported config was hidden")
	}
	missing := false
	changed := false
	for _, d := range working.Diagnostics {
		missing = missing || d.Reason == "boundary_unavailable"
		changed = changed || d.Reason == "configuration_change"
	}
	if !missing || changed {
		t.Fatalf("wrong config diagnostic: %+v", working.Diagnostics)
	}
	for _, name := range working.TestFiles {
		if strings.HasPrefix(name, "child/") {
			t.Fatal("selected dependency tests")
		}
	}
	for _, component := range working.Components {
		if strings.HasPrefix(component.Root, "child") {
			t.Fatal("discovered child component")
		}
	}
}
