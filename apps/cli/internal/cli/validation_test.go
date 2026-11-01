package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	repocli "github.com/compforge/repocli/toolkit/go"
)

func TestValidationCLIPlansAndRunsMake(t *testing.T) {
	root := t.TempDir()
	if output, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, output)
	}
	put(t, root, "pyproject.toml", "[project]\nname='fixture'\nversion='0.1.0'\n")
	put(t, root, ".gitignore", "*.out\n")
	put(t, root, "Makefile", "lint:\n\t@echo lint > lint.out\ntest:\n\t@echo test > test.out\n")
	for _, name := range []string{"lint", "test", "validate"} {
		t.Run(name, func(t *testing.T) {
			var out, stderr bytes.Buffer
			if code := Execute(context.Background(), []string{name, "--repo", root, "--plan"}, nil, &out, &stderr); code != 0 {
				t.Fatalf("%d: %s", code, &stderr)
			}
			var plan repocli.ValidationPlan
			if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
				t.Fatal(err)
			}
			want := 1
			if name == "validate" {
				want = 2
			}
			if len(plan.Components) != 1 || len(plan.Components[0].Checks) != want {
				t.Fatalf("%s", &out)
			}
			out.Reset()
			stderr.Reset()
			if code := Execute(context.Background(), []string{name, "--repo", root, "--json"}, nil, &out, &stderr); code != 0 {
				t.Fatalf("%d: %s", code, &stderr)
			}
			var report repocli.ValidationReport
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			if !report.Complete || !report.Passed || len(report.Components[0].Checks) != want {
				t.Fatalf("%s", &out)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(root, "test.out")); err != nil {
		t.Fatal(err)
	}
	put(t, root, "Makefile", "test:\n\t@false\n")
	var out, stderr bytes.Buffer
	if code := Execute(context.Background(), []string{"test", "--repo", root, "--json"}, nil, &out, &stderr); code != 1 {
		t.Fatalf("exit=%d %s", code, &stderr)
	}
	out.Reset()
	stderr.Reset()
	if code := Execute(context.Background(), []string{"test", "--repo", root, "--timeout=0"}, nil, &out, &stderr); code != 2 {
		t.Fatalf("exit=%d %s", code, &stderr)
	}
}

func TestValidationCLIPlanDoesNotRunProjectCode(t *testing.T) {
	root := t.TempDir()
	if output, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, output)
	}
	put(t, root, "pyproject.toml", "[project]\nname='fixture'\nversion='0.1.0'\n")
	put(t, root, "Makefile", "EXPAND := $(shell touch expanded.out)\ntest:\n\t@touch ran.out\n")
	var out, stderr bytes.Buffer
	if code := Execute(context.Background(), []string{"test", "--repo", root, "--plan"}, nil, &out, &stderr); code != 0 {
		t.Fatalf("exit=%d %s", code, &stderr)
	}
	for _, name := range []string{"expanded.out", "ran.out"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("planning executed %s", name)
		}
	}
}
