package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitCommand(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=repocli-test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=repocli-test", "GIT_COMMITTER_EMAIL=test@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
	return string(out)
}

func put(t *testing.T, root, name, data string) {
	t.Helper()
	full := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitCommand(t, dir, "init", "-q", "-b", "main")
	put(t, dir, "source file.ts", "export function a() { return 1; }\nexport function b() { return 2; }\n")
	put(t, dir, "tests/a.test.ts", "import { a as unused } from '../source file';\n")
	put(t, dir, "tests/b.test.ts", "import { b } from '../source file';\n")
	put(t, dir, ".gitignore", "ignored.ts\n")
	gitCommand(t, dir, "add", ".")
	gitCommand(t, dir, "commit", "-qm", "fixture")
	return dir
}

func submoduleFixture(t *testing.T) (string, string) {
	t.Helper()
	origin := fixture(t)
	parent := fixture(t)
	gitCommand(t, parent, "-c", "protocol.file.allow=always", "submodule", "add", origin, "child")
	gitCommand(t, parent, "commit", "-qam", "submodule")
	return parent, filepath.Join(parent, "child")
}
