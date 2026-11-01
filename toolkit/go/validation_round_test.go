package repocli_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	repocli "github.com/compforge/repocli/toolkit/go"
)

func writeValidationFile(t *testing.T, root, name, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestValidationComponentReuseAndWitnesses(t *testing.T) {
	root := validationRepo(t, "lint:\n\t@true\n")
	for _, name := range []string{"a", "b"} {
		writeValidationFile(t, root, name+"/pyproject.toml", "[project]\nname = '"+name+"'\nversion = '0.1.0'\n")
		writeValidationFile(t, root, name+"/Makefile", "test:\n\t@echo executed >> runs.out\n")
		writeValidationFile(t, root, name+"/code.py", "pass\n")
	}
	writeValidationFile(t, root, "shared.cfg", "one")
	writeValidationFile(t, root, "receipt.out", "installed")
	req := repocli.ValidationRequest{Repository: root, Components: []string{"a"}, Checks: []string{"test"}, Inputs: map[string]map[string]repocli.CheckInputDeclaration{"a": {"test": {Components: []string{"a"}, Paths: []string{"shared.cfg"}, Witnesses: []string{"receipt.out"}}}}}
	first, err := repocli.Validate(context.Background(), req)
	if err != nil || !first.Passed {
		t.Fatalf("%+v %v", first, err)
	}
	req.Previous = []repocli.ValidationReport{first}
	writeValidationFile(t, root, "b/code.py", "# unrelated edit\n")
	second, err := repocli.Validate(context.Background(), req)
	if err != nil || !second.Passed || !second.Components[0].Checks[0].Reused || second.Snapshot == first.Snapshot {
		t.Fatalf("%+v %v", second, err)
	}
	if second.Components[0].Checks[0].CheckedAt != first.Components[0].Checks[0].CheckedAt {
		t.Fatal("reuse changed actual execution timestamp")
	}
	for _, name := range []string{"shared.cfg", "receipt.out", "a/new.py"} {
		req.Previous = []repocli.ValidationReport{second}
		writeValidationFile(t, root, name, "changed")
		second, err = repocli.Validate(context.Background(), req)
		if err != nil || !second.Passed || second.Components[0].Checks[0].Reused {
			t.Fatalf("%s: %+v %v", name, second, err)
		}
	}
}

func TestValidationFailedAndDisabledEvidenceNeverReuse(t *testing.T) {
	root := validationRepo(t, "test:\n\t@echo run >> runs.out\n\t@false\n")
	req := repocli.ValidationRequest{Repository: root, Checks: []string{"test"}}
	first, err := repocli.Validate(context.Background(), req)
	if err != nil || first.Passed || !first.InputsConsistent {
		t.Fatalf("%+v %v", first, err)
	}
	req.Previous = []repocli.ValidationReport{first}
	again, err := repocli.Validate(context.Background(), req)
	if err != nil || again.Components[0].Checks[0].Reused {
		t.Fatalf("%+v %v", again, err)
	}
	writeValidationFile(t, root, "Makefile", "test:\n\t@true\n")
	req.Inputs = map[string]map[string]repocli.CheckInputDeclaration{".": {"test": {DisableReuse: true}}}
	first, err = repocli.Validate(context.Background(), req)
	if err != nil || !first.Passed {
		t.Fatal(err)
	}
	req.Previous = []repocli.ValidationReport{first}
	again, err = repocli.Validate(context.Background(), req)
	if err != nil || again.Components[0].Checks[0].Reused {
		t.Fatalf("%+v %v", again, err)
	}
}

func TestValidationReceiptMutationInvalidatesRound(t *testing.T) {
	root := validationRepo(t, "test:\n\t@echo changed > receipt.out\n")
	writeValidationFile(t, root, "receipt.out", "original")
	report, err := repocli.Validate(context.Background(), repocli.ValidationRequest{Repository: root, Checks: []string{"test"}, Inputs: map[string]map[string]repocli.CheckInputDeclaration{".": {"test": {Witnesses: []string{"receipt.out"}}}}})
	if err != nil || report.InputsConsistent || report.Passed || report.Components[0].Checks[0].Status != "passed" {
		t.Fatalf("%+v %v", report, err)
	}
}

func TestValidationFixReplansAndHasNoCheckEvidence(t *testing.T) {
	root := validationRepo(t, "fix:\n\t@echo fixed > source.py\nlint:\n\t@grep -q fixed source.py\ntest:\n\t@grep -q fixed source.py\n")
	writeValidationFile(t, root, "source.py", "unformatted")
	fixed, err := repocli.Fix(context.Background(), repocli.ValidationRequest{Repository: root})
	if err != nil || !fixed.Passed || len(fixed.Components) != 0 || fixed.InputsConsistent {
		t.Fatalf("%+v %v", fixed, err)
	}
	writeValidationFile(t, root, "source.py", "unformatted again")
	report, err := repocli.Validate(context.Background(), repocli.ValidationRequest{Repository: root, Checks: []string{"test", "fix", "lint"}})
	if err != nil || !report.Passed || !report.InputsConsistent || len(report.Preparation) != 1 || len(report.Components[0].Checks) != 2 {
		t.Fatalf("%+v %v", report, err)
	}
	plan := validationPlan(t, root, "fix")
	if _, err := repocli.ExecuteValidation(context.Background(), plan); err == nil {
		t.Fatal("fix entered readonly evidence executor")
	}
}

func TestValidationMissingTargetDoesNotInvalidateSuccessfulCheck(t *testing.T) {
	root := validationRepo(t, "lint:\n\t@true\n")
	report, err := repocli.Validate(context.Background(), repocli.ValidationRequest{Repository: root})
	if err != nil || !report.InputsConsistent || !report.Complete || report.Passed || report.Components[0].Checks[0].Status != "passed" {
		t.Fatalf("%+v %v", report, err)
	}
	req := repocli.ValidationRequest{Repository: root, Checks: []string{"lint"}, Previous: []repocli.ValidationReport{report}}
	again, err := repocli.Validate(context.Background(), req)
	if err != nil || !again.Passed || !again.Components[0].Checks[0].Reused {
		t.Fatalf("%+v %v", again, err)
	}
}

func TestValidationBoundedConcurrency(t *testing.T) {
	root := validationRepo(t, "lint test:\n\t@python3 counter.py\n")
	writeValidationFile(t, root, "counter.py", `import fcntl, json, time
from pathlib import Path

def change(delta):
    with open('lock.out', 'a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        path = Path('count.out')
        active, peak = json.loads(path.read_text()) if path.exists() else [0, 0]
        active += delta
        path.write_text(json.dumps([active, max(peak, active)]))
change(1)
time.sleep(0.2)
change(-1)
`)
	for _, budget := range []int{1, 2} {
		os.Remove(filepath.Join(root, "count.out"))
		report, err := repocli.Validate(context.Background(), repocli.ValidationRequest{Repository: root, Concurrency: budget})
		if err != nil || !report.Passed {
			t.Fatalf("%+v %v", report, err)
		}
		value, err := os.ReadFile(filepath.Join(root, "count.out"))
		expected := "[0, 1]"
		if budget == 2 {
			expected = "[0, 2]"
		}
		if err != nil || strings.TrimSpace(string(value)) != expected {
			t.Fatalf("budget %d: %s %v", budget, value, err)
		}
	}
}

func TestValidationFixCreatedComponentJoinsImpactSeeds(t *testing.T) {
	root := validationRepo(t, "fix:\n\t@mkdir -p child\n\t@printf 'module example.com/child\\n' > child/go.mod\n\t@printf 'lint test:\\n\\t@touch ran.out\\n' > child/Makefile\nlint test:\n\t@true\n")
	writeValidationFile(t, root, "source.py", "before")
	for _, args := range [][]string{{"add", "."}, {"-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "baseline"}} {
		if output, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, output)
		}
	}
	writeValidationFile(t, root, "source.py", "after")
	report, err := repocli.Validate(context.Background(), repocli.ValidationRequest{Repository: root, Base: "HEAD", ChangedFiles: []string{"source.py"}, Checks: []string{"fix", "lint", "test"}})
	if err != nil || !report.Passed {
		t.Fatalf("%+v %v", report, err)
	}
	if _, err := os.Stat(filepath.Join(root, "child/ran.out")); err != nil {
		t.Fatal("generated child was not checked", err)
	}
	if report.Impact == nil {
		t.Fatal("original impact evidence omitted")
	}
}

func TestValidationFixSupportsExplicitFiles(t *testing.T) {
	root := validationRepo(t, "fix:\n\t@printf '%s' '$(LINT_FILES)' > files.out\n")
	writeValidationFile(t, root, "source.py", "pass")
	report, err := repocli.Fix(context.Background(), repocli.ValidationRequest{Repository: root, Files: []string{"source.py"}})
	if err != nil || !report.Passed || report.Preparation[0].Checks[0].Scope != "focused" {
		t.Fatalf("%+v %v", report, err)
	}
	files, err := os.ReadFile(filepath.Join(root, "files.out"))
	if err != nil || string(files) != "source.py" {
		t.Fatalf("%s %v", files, err)
	}
}
