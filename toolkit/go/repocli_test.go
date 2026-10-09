package repocli_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/compforge/repocli/toolkit/go"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=toolkit-test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=toolkit-test", "GIT_COMMITTER_EMAIL=test@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
}

func put(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init", "-q", "-b", "main")
	put(t, root, "go.mod", "module example/toolkit\n\ngo 1.26\n")
	put(t, root, "main.go", "package main\nfunc main() {}\n")
	put(t, root, "testdata/corpus/go.mod", "module example/corpus\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-qm", "fixture")
	return root
}

func TestLibraryHasNoCLIHistorySideEffects(t *testing.T) {
	root, home := fixture(t), t.TempDir()
	t.Setenv("HOME", home)
	req := repocli.InputRequest{Repository: root}
	if _, err := repocli.Inspect(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	snapshot, err := repocli.Snapshot(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := repocli.Graph(context.Background(), req, 10)
	if err != nil {
		t.Fatal(err)
	}
	if graph.Snapshot.Snapshot != snapshot.Snapshot || len(graph.Nodes) == 0 {
		t.Fatal("graph lost its captured input")
	}
	if _, err := repocli.Diff(context.Background(), repocli.DiffRequest{Repository: root}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".repocli")); !os.IsNotExist(err) {
		t.Fatal("library created CLI state", err)
	}
}

func TestDiffValidatesLibraryInputs(t *testing.T) {
	for _, req := range []repocli.DiffRequest{
		{MaxFiles: -1}, {MaxRelations: -1},
		{Head: "HEAD", Staged: true}, {Head: "HEAD", PatchFile: "patch"},
		{Staged: true, PatchFile: "patch"}, {PatchFile: "-"}, {TestDirs: []string{"../outside"}},
	} {
		req.Repository = filepath.Join(t.TempDir(), "missing")
		if _, err := repocli.Diff(context.Background(), req); err == nil || strings.Contains(err.Error(), "git ") {
			t.Fatalf("input should fail before I/O: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repocli.Inspect(ctx, repocli.InputRequest{Repository: fixture(t)}); err == nil {
		t.Fatal("ignored cancellation")
	}
}
