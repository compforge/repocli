package repocli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiffHonorsByteBudgetForCommitWorkspaceAndPatch(t *testing.T) {
	repository := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", repository}, args...)...)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %v", args, output, err)
		}
		return strings.TrimSpace(string(output))
	}
	git("init", "-q")
	git("config", "user.name", "Test User")
	git("config", "user.email", "test@example.org")
	git("config", "commit.gpgsign", "false")
	file := filepath.Join(repository, "a.go")
	if err := os.WriteFile(file, []byte("package a\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "a.go")
	git("commit", "-qm", "base")
	base := git("rev-parse", "HEAD")
	if err := os.WriteFile(file, []byte("package a\n// changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	patch := git("diff", "--no-ext-diff") + "\n"
	requests := []DiffRequest{
		{Repository: repository, Base: base},
		{Repository: repository, Base: base, PatchFile: "-", Stdin: strings.NewReader(patch)},
	}
	git("add", "a.go")
	git("commit", "-qm", "head")
	head := git("rev-parse", "HEAD")
	requests = append(requests, DiffRequest{Repository: repository, Base: base, Head: head})
	for i, request := range requests {

		request.MaxSnapshotBytes = 16
		if _, err := Diff(t.Context(), request); err == nil || !strings.Contains(err.Error(), "byte budget") {
			t.Fatalf("input %d budget error=%v", i, err)
		}
		request.MaxSnapshotBytes = 64
		if request.PatchFile == "-" {
			request.Stdin = strings.NewReader(patch)
		}
		if _, err := Diff(t.Context(), request); err != nil {
			t.Fatalf("input %d: %v", i, err)
		}
	}
	if err := (DiffRequest{MaxSnapshotBytes: -1}).Validate(); err == nil {
		t.Fatal("negative byte budget accepted")
	}
}
