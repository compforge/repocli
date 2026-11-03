package validation_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	repokit "github.com/compforge/repokit/toolkit/go"
)

func TestValidationHistoryOwnsReuseAndStatistics(t *testing.T) {
	root := validationRepo(t, "lint:\n\t@echo executed >> runs.out\n\t@echo checked\n")
	req := repokit.ValidationRequest{Repository: root, HistoryDir: t.TempDir(), Checks: []string{"lint"}}
	first, err := repokit.Validate(context.Background(), req)
	if err != nil || !first.Passed {
		t.Fatalf("%+v %v", first, err)
	}
	second, err := repokit.Validate(context.Background(), req)
	if err != nil || !second.Passed {
		t.Fatalf("%+v %v", second, err)
	}
	a, b := first.Components[0].Checks[0], second.Components[0].Checks[0]
	if !b.Reused || a.RunID == "" || b.RunID != a.RunID || !b.CheckedAt.Equal(a.CheckedAt) || b.Stdout != a.Stdout || first.InputID == second.InputID {
		t.Fatalf("%+v %+v", a, b)
	}
	if second.History == nil {
		t.Fatal(second.Diagnostics)
	}
	if c := second.History.Checks[". lint"]; c.Executions != 1 || c.Reuses != 1 || c.ExecutionSeconds != a.DurationSeconds {
		t.Fatalf("%+v", c)
	}
	data, _ := json.Marshal(second)
	if strings.Contains(string(data), "fingerprint") {
		t.Fatal("per-check fingerprint survived")
	}
	req.Force = true
	forced, err := repokit.Validate(context.Background(), req)
	if err != nil || !forced.Passed || forced.Components[0].Checks[0].Reused {
		t.Fatalf("%+v %v", forced, err)
	}
	stats, err := repokit.ReadValidationHistory(context.Background(), req.HistoryDir, root)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Checks[". lint"].Executions != 2 || stats.Checks[". lint"].Reuses != 1 {
		t.Fatal(stats)
	}
	actual, _ := os.ReadFile(filepath.Join(root, "runs.out"))
	if string(actual) != "executed\nexecuted\n" {
		t.Fatalf("%q", actual)
	}
}

func TestValidationHistoryEnvironmentAndScopeInvalidate(t *testing.T) {
	root := validationRepo(t, "lint:\n\t@echo '$(LINT_FILES)' >> runs.out\n")
	writeValidationFile(t, root, "a.py", "pass")
	writeValidationFile(t, root, "b.py", "pass")
	req := repokit.ValidationRequest{Repository: root, HistoryDir: t.TempDir(), Checks: []string{"lint"}, Files: []string{"a.py"}, Environment: []string{"PATH=" + os.Getenv("PATH"), "MODE=one"}}
	run := func(reused bool) {
		t.Helper()
		report, err := repokit.Validate(context.Background(), req)
		if err != nil || !report.Passed || report.Components[0].Checks[0].Reused != reused {
			t.Fatalf("%+v %v", report, err)
		}
	}
	run(false)
	run(true)
	req.Environment[1] = "MODE=two"
	run(false)
	req.Files = []string{"b.py"}
	run(false)
	run(true)
	writeValidationFile(t, root, "b.py", "# changed")
	run(false)
}

func TestValidationCorruptHistoryRunsChecksAndReportsGap(t *testing.T) {
	root := validationRepo(t, "lint:\n\t@echo run >> runs.out\n")
	req := repokit.ValidationRequest{Repository: root, HistoryDir: t.TempDir(), Checks: []string{"lint"}}
	first, err := repokit.Validate(context.Background(), req)
	if err != nil || !first.Passed {
		t.Fatalf("%+v %v", first, err)
	}
	paths, err := filepath.Glob(filepath.Join(req.HistoryDir, "*", "history", "*"))
	if err != nil || len(paths) == 0 {
		t.Fatal(err)
	}
	file, err := os.OpenFile(paths[len(paths)-1], os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.WriteString("{")
	if err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	next, err := repokit.Validate(context.Background(), req)
	if err != nil || !next.Passed || next.Components[0].Checks[0].Reused || len(next.Diagnostics) == 0 || next.History != nil {
		t.Fatalf("%+v %v", next, err)
	}
	actual, _ := os.ReadFile(filepath.Join(root, "runs.out"))
	if string(actual) != "run\nrun\n" {
		t.Fatalf("%q", actual)
	}
}

func TestValidationUnavailableExecutableDoesNotCountAsExecution(t *testing.T) {
	root := validationRepo(t, "lint:\n\t@true\n")
	req := repokit.ValidationRequest{Repository: root, HistoryDir: t.TempDir(), Checks: []string{"lint"}, Environment: []string{"PATH=/no-such-path"}}
	report, err := repokit.Validate(context.Background(), req)
	if err != nil || report.Components[0].Checks[0].Status != "unavailable" || report.History == nil || len(report.History.Checks) != 0 {
		t.Fatalf("%+v %v", report, err)
	}
}

// +case:id=repository_history_worktrees,desc=`Two worktrees validate the same repository`,expect=`One history directory aggregates runs while reuse stays within each checkout`
func TestValidationRepositoryHistoryAcrossWorktrees(t *testing.T) {
	for _, origin := range []string{"", "https://github.com/example/shared.git"} {
		t.Run(origin, func(t *testing.T) {
			ctx := context.Background()
			root := validationRepo(t, "lint:\n\t@echo executed >> runs.out\n")
			git := func(args ...string) {
				t.Helper()
				cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v: %s", args, err, output)
				}
			}
			if origin != "" {
				git("remote", "add", "origin", origin)
			}
			git("add", ".")
			git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "-qm", "fixture")
			linked := filepath.Join(t.TempDir(), "linked")
			git("worktree", "add", "--detach", linked, "HEAD")
			var err error
			linked, err = filepath.EvalSymlinks(linked)
			if err != nil {
				t.Fatal(err)
			}
			history := t.TempDir()
			ids := map[string]string{}
			for i, checkout := range []string{root, linked, root, linked} {
				report, err := repokit.Validate(ctx, repokit.ValidationRequest{Repository: checkout, HistoryDir: history, Checks: []string{"lint"}})
				if err != nil || !report.Passed || !report.InputsConsistent || report.History == nil {
					t.Fatalf("%+v: %v", report, err)
				}
				result := report.Components[0].Checks[0]
				if result.Reused != (i >= 2) {
					t.Fatalf("checkout=%s reused=%v", checkout, result.Reused)
				}
				if i < 2 {
					ids[checkout] = result.RunID
				} else if result.RunID != ids[checkout] {
					t.Fatal("reused another checkout")
				}
			}
			if ids[root] == ids[linked] {
				t.Fatal("cross-checkout run reused")
			}
			for _, checkout := range []string{root, linked} {
				stats, err := repokit.ReadValidationHistory(ctx, history, checkout)
				if err != nil {
					t.Fatal(err)
				}
				counts := stats.Checks[". lint"]
				if counts.Executions != 2 || counts.Reuses != 2 {
					t.Fatalf("%+v", counts)
				}
				actual, err := os.ReadFile(filepath.Join(checkout, "runs.out"))
				if err != nil || string(actual) != "executed\n" {
					t.Fatalf("%s: %v", actual, err)
				}
			}
			entries, err := os.ReadDir(history)
			if err != nil || len(entries) != 1 {
				t.Fatalf("repository directories=%v: %v", entries, err)
			}
			if origin != "" && entries[0].Name() != "github--example--shared" {
				t.Fatal(entries[0].Name())
			}
			stored, err := os.ReadDir(filepath.Join(history, entries[0].Name()))
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range stored {
				switch entry.Name() {
				case "history", "inputs", "logs", "lock":
				default:
					t.Fatalf("unexpected per-checkout storage: %s", entry.Name())
				}
			}
		})
	}
}
