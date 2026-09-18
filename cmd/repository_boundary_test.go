package cmd

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestUntrackedRepositoriesStayOutsideParentCapture(t *testing.T) {
	for _, kind := range []string{"linked_worktree", "nested_repository"} {
		t.Run(kind, func(t *testing.T) {
			dir := fixture(t)
			baseline := snapshotJSON(t, dir)
			nested := filepath.Join(dir, "checkouts", "child")
			if kind == "linked_worktree" {
				gitCommand(t, dir, "worktree", "add", "--detach", nested, "HEAD")
			} else {
				gitCommand(t, dir, "init", "-q", nested)
			}
			put(t, nested, "private.py", "def child_only(): return 1\n")
			listing := gitCommand(t, dir, "ls-files", "-z", "--others", "--exclude-standard")
			if !strings.Contains(listing, "checkouts/child/\x00") {
				t.Fatalf("fixture lacks Git directory entry: %q", listing)
			}
			status := gitCommand(t, dir, "status", "--porcelain=v1")
			captured := snapshotJSON(t, dir)
			if !captured.Complete || captured.Snapshot != baseline.Snapshot || captured.FileCount != baseline.FileCount {
				t.Fatalf("nested contents entered parent: %+v", captured)
			}
			t.Chdir(dir)
			report := runJSON(t, []string{"diff", "--json"}, "")
			if !report.Complete || len(report.Changes) != 0 || report.Snapshot != captured.Snapshot {
				t.Fatalf("default diff: %+v", report)
			}
			if gitCommand(t, dir, "status", "--porcelain=v1") != status {
				t.Fatal("capture changed parent status")
			}
			// A sibling file is still owned by the parent, even under .worktrees.
			put(t, dir, ".worktrees/notes.py", "def parent_owned(): return 1\n")
			report = runJSON(t, []string{"diff", "--json"}, "")
			if !report.Complete || !reflect.DeepEqual(report.SourceFiles, []string{".worktrees/notes.py"}) {
				t.Fatalf("lost ordinary untracked source: %+v", report)
			}
			if snapshotJSON(t, dir).Snapshot == baseline.Snapshot {
				t.Fatal("ordinary untracked contents missing from identity")
			}
			child := snapshotJSON(t, nested)
			if !child.Complete || child.Checkout == captured.Checkout {
				t.Fatal("cannot analyze nested checkout directly")
			}
		})
	}
}
