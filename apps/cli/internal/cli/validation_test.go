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

func TestValidationCLIFormatCompositionAndReuseReport(t *testing.T) {
	root := t.TempDir()
	if output, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, output)
	}
	put(t, root, "pyproject.toml", "[project]\nname='fixture'\nversion='0.1.0'\n")
	put(t, root, ".gitignore", "*.out\n")
	put(t, root, "source.py", "not formatted")
	put(t, root, "Makefile", "format:\n\t@echo formatted > source.py\nlint test:\n\t@grep -q formatted source.py\n")
	run := func(args ...string) (int, repocli.ValidationReport) {
		t.Helper()
		var out, stderr bytes.Buffer
		code := Execute(context.Background(), append(args, "--repo", root, "--json"), nil, &out, &stderr)
		var report repocli.ValidationReport
		if err := json.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatalf("%d %s %s %v", code, &out, &stderr, err)
		}
		return code, report
	}
	if code, report := run("format"); code != 0 || len(report.Components) != 0 || len(report.Preparation) != 1 {
		t.Fatalf("%d %+v", code, report)
	}
	previous := filepath.Join(root, "previous.out")
	code, report := run("validate", "--check", "test,format,lint", "--output", previous)
	if code != 0 || !report.InputsConsistent || len(report.Preparation) != 1 {
		t.Fatalf("%d %+v", code, report)
	}
	code, report = run("validate", "--reuse", previous)
	if code != 0 || !report.Components[0].Checks[0].Reused || !report.Components[0].Checks[1].Reused {
		t.Fatalf("%d %+v", code, report)
	}
	put(t, root, "Makefile", "format:\n\t@false\nlint:\n\t@touch unexpected.out\n")
	code, report = run("validate", "--check", "format,lint")
	if code != 1 || len(report.Components) != 0 {
		t.Fatalf("%d %+v", code, report)
	}
	if _, err := os.Stat(filepath.Join(root, "unexpected.out")); !os.IsNotExist(err) {
		t.Fatal("lint ran after failed format")
	}
}

func TestValidationCLIRootFallbackDeduplicatesExplicitComponents(t *testing.T) {
	root := t.TempDir()
	if output, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, output)
	}
	put(t, root, "pyproject.toml", "[project]\nname='fixture'\nversion='0.1.0'\n")
	put(t, root, ".gitignore", "*.out\n")
	put(t, root, "Makefile", "test:\n\t@echo run >> runs.out\n")
	for _, child := range []string{"a", "b"} {
		put(t, root, child+"/pyproject.toml", "[project]\nname='"+child+"'\nversion='0.1.0'\n")
	}
	var out, stderr bytes.Buffer
	args := []string{"test", "--repo", root, "--component", "a", "--component", "b", "--json"}
	if code := Execute(context.Background(), args, nil, &out, &stderr); code != 0 {
		t.Fatalf("exit=%d %s", code, &stderr)
	}
	var report repocli.ValidationReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Passed || len(report.Components) != 1 || report.Components[0].Component.Root != "." || len(report.Components[0].Checks) != 1 {
		t.Fatalf("%s", &out)
	}
	sources := report.Components[0].Checks[0].RequestedComponents
	if len(sources) != 2 || sources[0] != "a" || sources[1] != "b" {
		t.Fatalf("%v", sources)
	}
	runs, err := os.ReadFile(filepath.Join(root, "runs.out"))
	if err != nil || string(runs) != "run\n" {
		t.Fatalf("runs=%q %v", runs, err)
	}
}
