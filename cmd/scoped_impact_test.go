package cmd

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDiffUnchangedInstructionLinkDoesNotForceFallback(t *testing.T) {
	dir := fixture(t)
	put(t, dir, "AGENTS.md", "instructions")
	if err := os.Symlink("AGENTS.md", filepath.Join(dir, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, dir, "add", ".")
	gitCommand(t, dir, "commit", "-qm", "instructions")
	put(t, dir, "source file.ts", "export function a(){return 4;}\n")
	got := runJSON(t, []string{"diff", "--repo", dir, "--test-dir", "tests", "--json"}, "")
	if !got.Complete || got.Scope != "focused" {
		t.Fatal(got)
	}
}

func TestDiffKeepsSubmoduleAsParentGitlink(t *testing.T) {
	parent, child := submoduleFixture(t)
	put(t, parent, "tests/child.test.ts", "import {a} from '../child/source file';\n")
	gitCommand(t, parent, "add", ".")
	gitCommand(t, parent, "commit", "-qm", "consumer")
	put(t, child, "source file.ts", "export function a(){return 9;}\n")
	put(t, child, "new_test.py", "def test_child(): pass\n")
	got := runJSON(t, []string{"diff", "--repo", parent, "--test-dir", ".", "--json"}, "")
	if !got.Complete {
		t.Fatal(got)
	}
	found := false
	for _, change := range got.Changes {
		if change.Path == "child" {
			found = true
		}
		if strings.HasPrefix(change.Path, "child/") {
			t.Fatalf("expanded gitlink: %+v", change)
		}
	}
	if !found || !slices.Contains(got.TestFiles, "tests/child.test.ts") {
		t.Fatal(got)
	}
	for _, name := range got.TestFiles {
		if strings.HasPrefix(name, "child/") {
			t.Fatalf("selected dependency tests: %s", name)
		}
	}
	for _, group := range got.Components {
		if group.Root == "child" || strings.HasPrefix(group.Root, "child/") {
			t.Fatalf("discovered dependency component: %+v", group)
		}
	}
}

func TestUnchangedSubmoduleDoesNotForceFallback(t *testing.T) {
	parent, _ := submoduleFixture(t)
	put(t, parent, "tests/child.test.ts", "import {a} from '../child/source file';\n")
	gitCommand(t, parent, "add", ".")
	gitCommand(t, parent, "commit", "-qm", "consumer")
	put(t, parent, "source file.ts", "export function a(){return 7;}\n")
	got := runJSON(t, []string{"diff", "--repo", parent, "--test-dir", ".", "--json"}, "")
	if !got.Complete || got.Scope != "focused" || slices.Contains(got.TestFiles, "tests/child.test.ts") {
		t.Fatal(got)
	}
}

func TestPartialReportOutputsOnlyKnownTestRelationships(t *testing.T) {
	dir := fixture(t)
	put(t, dir, "tests/dynamic.test.ts", "const load=()=>import(target);\n")
	gitCommand(t, dir, "add", ".")
	gitCommand(t, dir, "commit", "-qm", "dynamic test")
	put(t, dir, "source file.ts", "export function a() { return 7; }\nexport function b() { return 2; }\n")
	got := runJSON(t, []string{"diff", "--repo", dir, "--test-dir", ".", "--json"}, "")
	if got.Complete || got.Scope != "partial" || !slices.Equal(got.TestFiles, []string{"tests/a.test.ts"}) {
		t.Fatal(got)
	}
	if !slices.Equal(got.SourceFiles, []string{"source file.ts"}) || len(got.Diagnostics) == 0 {
		t.Fatal(got)
	}
	for _, reason := range got.Reasons {
		if reason.Kind == "fallback" {
			t.Fatalf("invented association: %+v", reason)
		}
	}
}
