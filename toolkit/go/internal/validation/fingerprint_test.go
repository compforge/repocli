package validation_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	repokit "github.com/compforge/repokit/toolkit/go"
)

// +case:id=version_independent_check_reuse,desc=`Version Documents change between validation rounds`,expect=`Checks reuse without execution; full snapshots change and selected files remain identical`
func TestValidationVersionDocumentsDoNotInvalidateChecks(t *testing.T) {
	for _, mode := range []string{"repository", "declared", "root-fallback", "symlink-fallback"} {
		t.Run(mode, func(t *testing.T) {
			root := validationRepo(t, "lint test:\n\t@echo '$(LINT_FILES) $(TEST_FILES)' >> runs.out\n")
			writeValidationFile(t, root, "code.py", "pass\n")
			writeValidationFile(t, root, "VERSION", "1.0.0\n")
			req := repokit.ValidationRequest{Repository: root, Files: []string{"code.py"}}
			if mode != "repository" {
				declaration := repokit.CheckInputDeclaration{Paths: []string{"VERSION"}}
				req.Inputs = map[string]map[string]repokit.CheckInputDeclaration{".": {"lint": declaration, "test": declaration}}
			}
			if mode == "root-fallback" {
				fallbackChild(t, root, "child", "")
				req.Components, req.Files = []string{"child"}, []string{"child/code.py"}
			}
			if mode == "symlink-fallback" {
				if err := os.Symlink("code.py", filepath.Join(root, "linked.py")); err != nil {
					t.Fatal(err)
				}
			}
			first, err := repokit.Validate(context.Background(), req)
			if err != nil || !first.Passed {
				t.Fatalf("%+v %v", first, err)
			}
			if mode == "symlink-fallback" && !strings.Contains(first.Components[0].Checks[0].InputReason, "symlinks") {
				t.Fatal("fixture did not exercise symlink fallback")
			}
			runs, err := os.ReadFile(filepath.Join(root, "runs.out"))
			if err != nil {
				t.Fatal(err)
			}
			previous := first
			for _, change := range []string{"edit", "add", "remove"} {
				switch change {
				case "edit":
					writeValidationFile(t, root, "VERSION", "1.0.1\n")
				case "add":
					writeValidationFile(t, root, "release/VERSION", "2.0.0\n")
				case "remove":
					if err := os.Remove(filepath.Join(root, "VERSION")); err != nil {
						t.Fatal(err)
					}
				}
				req.Previous = []repokit.ValidationReport{previous}
				next, err := repokit.Validate(context.Background(), req)
				if err != nil || !next.Passed || !next.InputsConsistent || next.Snapshot == previous.Snapshot {
					t.Fatalf("%s: %+v %v", change, next, err)
				}
				for i, check := range next.Components[0].Checks {
					before := previous.Components[0].Checks[i]
					if !check.Reused || check.Fingerprint != before.Fingerprint || check.CheckedAt != before.CheckedAt || check.Scope != "focused" || !reflect.DeepEqual(check.Files, before.Files) {
						t.Fatalf("%s changed check evidence: %+v", change, check)
					}
				}
				actual, err := os.ReadFile(filepath.Join(root, "runs.out"))
				if err != nil || string(actual) != string(runs) {
					t.Fatalf("%s reran checks: %q %v", change, actual, err)
				}
				previous = next
			}
			req.Previous = []repokit.ValidationReport{previous}
			writeValidationFile(t, root, req.Files[0], "# code changed\n")
			changed, err := repokit.Validate(context.Background(), req)
			if err != nil || !changed.Passed {
				t.Fatalf("%+v %v", changed, err)
			}
			for i, check := range changed.Components[0].Checks {
				if check.Reused || check.Fingerprint == previous.Components[0].Checks[i].Fingerprint {
					t.Fatalf("code edit reused stale evidence: %+v", check)
				}
			}
		})
	}
}

// +case:id=version_snapshot_consistency,desc=`Version changes after planning or during execution`,expect=`Full snapshot guards invalidate the round despite check fingerprint exclusions`
func TestValidationVersionChangesStillInvalidateFrozenRounds(t *testing.T) {
	root := validationRepo(t, "test:\n\t@echo run >> runs.out\n")
	writeValidationFile(t, root, "VERSION", "1\n")
	plan := validationPlan(t, root, "test")
	writeValidationFile(t, root, "VERSION", "2\n")
	report, err := repokit.ExecuteValidation(context.Background(), plan)
	if err != nil || report.InputsConsistent || report.Passed || len(report.Components) != 0 {
		t.Fatalf("stale snapshot accepted: %+v %v", report, err)
	}
	if _, err := os.Stat(filepath.Join(root, "runs.out")); !os.IsNotExist(err) {
		t.Fatal("stale plan executed", err)
	}
	writeValidationFile(t, root, "Makefile", "test:\n\t@echo 3 > VERSION\n")
	report, err = repokit.Validate(context.Background(), repokit.ValidationRequest{Repository: root, Checks: []string{"test"}})
	if err != nil || report.InputsConsistent || report.Passed || report.Components[0].Checks[0].Status != "passed" {
		t.Fatalf("concurrent version change accepted: %+v %v", report, err)
	}
}

func TestValidationFingerprintWireNameAndLegacyCacheMiss(t *testing.T) {
	root := validationRepo(t, "test:\n\t@echo run >> runs.out\n")
	req := repokit.ValidationRequest{Repository: root, Checks: []string{"test"}}
	first, err := repokit.Validate(context.Background(), req)
	if err != nil || !first.Passed {
		t.Fatalf("%+v %v", first, err)
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"fingerprint":"sha256:`) || strings.Contains(string(encoded), `"inputFingerprint"`) {
		t.Fatalf("unexpected wire name: %s", encoded)
	}
	var decoded repokit.ValidationReport
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	req.Previous = []repokit.ValidationReport{decoded}
	current, err := repokit.Validate(context.Background(), req)
	if err != nil || !current.Passed || !current.Components[0].Checks[0].Reused {
		t.Fatalf("%+v %v", current, err)
	}
	legacy := strings.ReplaceAll(string(encoded), `"fingerprint":`, `"inputFingerprint":`)
	decoded = repokit.ValidationReport{}
	if err := json.Unmarshal([]byte(legacy), &decoded); err != nil {
		t.Fatal(err)
	}
	req.Previous = []repokit.ValidationReport{decoded}
	current, err = repokit.Validate(context.Background(), req)
	if err != nil || !current.Passed || current.Components[0].Checks[0].Reused {
		t.Fatalf("legacy evidence reused: %+v %v", current, err)
	}
}
