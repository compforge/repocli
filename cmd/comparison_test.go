package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCommittedAndStagedSnapshotsIgnoreOtherEdits(t *testing.T) {
	dir := fixture(t)
	base := strings.TrimSpace(gitCommand(t, dir, "rev-parse", "HEAD"))
	put(t, dir, "source file.ts", "export function a() { return 9; }\nexport function b() { return 2; }\n")
	gitCommand(t, dir, "add", "source file.ts")
	put(t, dir, "source file.ts", "bad syntax {\n")
	args := []string{"diff", "--repo", dir, "--base", base, "--test-dir", ".", "--json"}
	indexBefore := gitCommand(t, dir, "ls-files", "--stage")
	staged := runJSON(t, append(args, "--staged"), "")
	if staged.Input != "index" || !staged.Complete || staged.SchemaVersion != 3 {
		t.Fatalf("%+v", staged)
	}
	gitCommand(t, dir, "commit", "-qm", "change")
	commit := runJSON(t, append(args, "--head", "HEAD"), "")
	if commit.Input != "commit" || !commit.Complete || commit.Snapshot != staged.Snapshot {
		t.Fatalf("%+v", commit)
	}
	if gitCommand(t, dir, "ls-files", "--stage") != indexBefore {
		t.Fatal("index changed")
	}
}

func TestAutomaticImpactAndSeedFilterRetainConsumers(t *testing.T) {
	dir := fixture(t)
	put(t, dir, "source file.ts", "export function a() { return 9; }\nexport function b() { return a(); }\n")
	put(t, dir, "tests/unrelated.test.ts", "export const test = 1;\n")
	r := runJSON(t, []string{"diff", "--repo", dir, "--test-dir", ".", "--changed-file", "source file.ts", "--json"}, "")
	if !r.Complete || !reflect.DeepEqual(r.TestFiles, []string{"tests/a.test.ts", "tests/b.test.ts"}) || len(r.SourceFiles) != 1 {
		t.Fatalf("%+v", r)
	}
}

func TestSnapshotGapsAreReportedWithoutImpactOrChanges(t *testing.T) {
	dir := fixture(t)
	if err := os.Symlink("source file.ts", filepath.Join(dir, "linked.ts")); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, dir, "add", "linked.ts")
	gitCommand(t, dir, "commit", "-qm", "link")
	for _, extra := range [][]string{nil, {"--test-dir", "."}} {
		r := runJSON(t, append([]string{"diff", "--repo", dir, "--json"}, extra...), "")
		if r.Complete || len(r.Diagnostics) == 0 || len(r.Changes) != 0 || r.Diagnostics[0].Path != "linked.ts" {
			t.Fatalf("%+v", r)
		}
	}
}

func TestInputExclusivityAndVersion(t *testing.T) {
	for _, args := range [][]string{{"diff", "--head", "HEAD", "--staged"}, {"diff", "--file", "-", "--head", "HEAD"}, {"diff", "--impact", "unknown"}} {
		var out, stderr bytes.Buffer
		if code := Execute(context.Background(), args, strings.NewReader(""), &out, &stderr); code != 2 {
			t.Fatalf("%v: %d %s", args, code, stderr.String())
		}
	}
	var out, stderr bytes.Buffer
	if code := Execute(context.Background(), []string{"--version"}, nil, &out, &stderr); code != 0 || !strings.Contains(out.String(), Version) {
		t.Fatal(out.String(), stderr.String())
	}
}

func TestLocalOutlineGapDoesNotDegradeReportOrComponent(t *testing.T) {
	dir := fixture(t)
	put(t, dir, "agent.ts", "export class Agent { private async *retry(text: string): AsyncGenerator<Event> {} }\n")
	put(t, dir, "tests/a.test.ts", "import { a } from '../source file';\nimport { Agent } from '../agent';\n")
	gitCommand(t, dir, "add", ".")
	gitCommand(t, dir, "commit", "-qm", "outline gap fixture")
	put(t, dir, "source file.ts", "export function a() { return 3; }\nexport function b() { return 2; }\n")
	r := runJSON(t, []string{"diff", "--repo", dir, "--test-dir", "tests", "--json"}, "")
	if !r.Complete || r.Scope != "focused" || len(r.Diagnostics) != 0 || len(r.Observations) != 2 || len(r.TestFiles) != 1 {
		t.Fatalf("local gap changed execution completeness: %+v", r)
	}
	for _, component := range r.Components {
		if !component.Complete || component.Scope != "focused" {
			t.Fatalf("component degraded: %+v", component)
		}
	}
}
