package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktreeCleanForwardsExplicitOptions(t *testing.T) {
	runtimeDir := t.TempDir()
	path := filepath.Join(runtimeDir, "repokit-worktree")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", runtimeDir)
	t.Setenv("HOME", t.TempDir())
	var out, stderr bytes.Buffer
	args := []string{"worktree", "clean", "--repo", "/repo with spaces", "--dry-run", "--json", "--max-count", "4", "--max-prepared", "0", "--target-repository", "upstream/repo"}
	code := Execute(context.Background(), args, nil, &out, &stderr)
	if code != 0 {
		t.Fatalf("%d: %s", code, stderr.String())
	}
	want := "--repo\n/repo with spaces\n--dry-run\n--json\n--max-count\n4\n--max-prepared\n0\n--target-repository\nupstream/repo\n"
	if out.String() != want {
		t.Fatalf("got %q, want %q", out.String(), want)
	}
}

func TestWorktreeCleanMissingRuntimeAndFailedOperation(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("PATH", runtimeDir)
	t.Setenv("HOME", t.TempDir())
	var out, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"worktree", "clean"}, nil, &out, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "uv tool install") {
		t.Fatalf("%d %s", code, &stderr)
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "repokit-worktree"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if code := Execute(context.Background(), []string{"worktree", "clean"}, nil, &out, &stderr); code != 1 {
		t.Fatalf("failed operation reported success: %d", code)
	}
}
