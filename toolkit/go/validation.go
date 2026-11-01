package repocli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	cg "github.com/compforge/codegraph"
)

// ValidationRequest selects Makefile-defined checks. Files are repository-relative
// candidates, not shell arguments. Nil components means all discovered Components;
// an explicitly empty slice means none. An impact report is supplied, never obtained
// implicitly by executing validation. Tags selects affected file roles for automatic checks.
type ValidationRequest struct {
	Repository  string
	Components  []string
	Checks      []string
	Files       []string
	Impact      *ImpactReport
	Tags        []string
	Environment []string // nil inherits the caller's environment; never serialized in results.
}

// CheckSpec freezes one Component/check invocation. Scope is full, focused,
// skipped or unavailable. The executor does not rediscover targets or select files.
type CheckSpec struct {
	Check       string   `json:"check"`
	Scope       string   `json:"scope"`
	Command     []string `json:"command"`
	Files       []string `json:"files"`
	Reason      string   `json:"reason"`
	Environment string   `json:"environment"` // digest only; values can contain secrets.
}

type ComponentValidationPlan struct {
	Component ComponentBinding `json:"component"`
	Checks    []CheckSpec      `json:"checks"`
}

// ValidationPlan is a live, content-bound plan. JSON is an observation, not an
// executable artifact: runtime environment and the integrity seal are private.
type ValidationPlan struct {
	Checkout    string                    `json:"checkout"`
	Snapshot    string                    `json:"snapshot"`
	Components  []ComponentValidationPlan `json:"components"`
	environment []string
	seal        string
}

// CheckResult records command outcome separately from round-level consistency.
// A passed command is not reusable evidence when its ValidationReport is incomplete.
type CheckResult struct {
	CheckSpec
	Status          string  `json:"status"`
	ExitCode        *int    `json:"exitCode"`
	DurationSeconds float64 `json:"durationSeconds"`
	Stdout          string  `json:"stdout"`
	Stderr          string  `json:"stderr"`
	Diagnostic      string  `json:"diagnostic,omitempty"`
}

type ComponentValidationResult struct {
	Component ComponentBinding `json:"component"`
	Checks    []CheckResult    `json:"checks"`
}

// ValidationReport reports execution, not permission to commit or merge.
// Complete means every selected check finished and inputs stayed unchanged;
// Passed additionally requires that every executed check succeeded.
type ValidationReport struct {
	Checkout    string                      `json:"checkout"`
	Snapshot    string                      `json:"snapshot"`
	Components  []ComponentValidationResult `json:"components"`
	Complete    bool                        `json:"complete"`
	Passed      bool                        `json:"passed"`
	Diagnostics []string                    `json:"diagnostics"`
}

var makeFilePath = regexp.MustCompile(`^[A-Za-z0-9_./@+][A-Za-z0-9_./@+:-]*$`)

// PlanValidation only reads the checkout. Preparation/formatting must precede it.
// +spec=Makefile is the execution contract; absent targets never select language-specific commands
// +spec=Input fingerprints bind execution evidence and never decide affected-file selection
// +link=component://docs/validation.md
func PlanValidation(ctx context.Context, req ValidationRequest) (ValidationPlan, error) {
	observed, err := Snapshot(ctx, InputRequest{Repository: req.Repository})
	if err != nil {
		return ValidationPlan{}, err
	}
	if !observed.Complete {
		return ValidationPlan{}, errors.New("validation snapshot is incomplete")
	}
	layout, err := Inspect(ctx, InputRequest{Repository: observed.Checkout})
	if err != nil {
		return ValidationPlan{}, err
	}
	if !layout.Complete {
		return ValidationPlan{}, errors.New("validation layout is incomplete")
	}
	checks := req.Checks
	if checks == nil {
		checks = []string{"lint", "test"}
	}
	checks = slices.Clone(checks)
	sort.Strings(checks)
	checks = slices.Compact(checks)
	for _, check := range checks {
		if check != "lint" && check != "test" {
			return ValidationPlan{}, fmt.Errorf("unsupported check %q", check)
		}
	}
	if len(req.Files) > 0 && req.Impact != nil {
		return ValidationPlan{}, errors.New("files and impact are alternative selections")
	}
	if req.Impact != nil && (req.Impact.Snapshot != observed.Snapshot || req.Impact.Input != "working_tree" || req.Impact.Checkout != observed.Checkout) {
		return ValidationPlan{}, errors.New("impact does not describe current checkout contents")
	}
	env := req.Environment
	if env == nil {
		env = os.Environ()
	}
	// Make's inherited flags/overrides can turn a frozen full command into a dry-run,
	// another Makefile or a narrowed check. These control inputs belong to the plan.
	envMap := map[string]string{}
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" || strings.ContainsRune(entry, '\x00') {
			return ValidationPlan{}, errors.New("invalid execution environment")
		}
		switch key {
		case "MAKEFILES", "MAKEFLAGS", "MFLAGS", "MAKEOVERRIDES", "GNUMAKEFLAGS", "MAKELEVEL", "LINT_FILES", "TEST_FILES":
			continue
		}
		envMap[key] = value
	}
	env = make([]string, 0, len(envMap))
	for k, v := range envMap {
		env = append(env, k+"="+v)
	}
	sort.Strings(env)
	encoded, _ := json.Marshal(env)
	digest := sha256.Sum256(encoded)
	plan := ValidationPlan{Checkout: observed.Checkout, Snapshot: observed.Snapshot, Components: []ComponentValidationPlan{}, environment: env}
	selected := map[string]bool{}
	for _, root := range req.Components {
		selected[root] = false
	}
	tags := req.Tags
	if tags == nil {
		tags = []string{"source", "test", "config", "manifest", "lockfile"}
	}
	for _, component := range layout.Components {
		if req.Components != nil {
			if _, ok := selected[component.Root]; !ok {
				continue
			}
			selected[component.Root] = true
		}
		directory := filepath.Join(plan.Checkout, component.Root)
		real, err := filepath.EvalSymlinks(directory)
		if err != nil {
			return ValidationPlan{}, err
		}
		if real != directory {
			return ValidationPlan{}, fmt.Errorf("Component %q crosses a symlink", component.Root)
		}
		makefile := filepath.Join(directory, "Makefile")
		info, statErr := os.Lstat(makefile)
		if statErr == nil && !info.Mode().IsRegular() {
			return ValidationPlan{}, fmt.Errorf("Component %q Makefile must be a regular file", component.Root)
		}
		text, err := os.ReadFile(makefile)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return ValidationPlan{}, err
		}
		contract := readMakeContract(ctx, text)
		group := ComponentValidationPlan{Component: component, Checks: []CheckSpec{}}
		for _, check := range checks {
			spec := CheckSpec{Check: check, Scope: "full", Command: []string{}, Files: []string{}, Reason: "full Component check", Environment: hex.EncodeToString(digest[:])}
			candidates := slices.Clone(req.Files)
			if req.Impact != nil {
				var impact *ComponentImpact
				for i := range req.Impact.Components {
					item := &req.Impact.Components[i]
					if item.Root == component.Root && item.Snapshot == "after" {
						impact = item
						break
					}
				}
				if impact == nil {
					return ValidationPlan{}, fmt.Errorf("impact missing current Component %q", component.Root)
				}
				if impact.AffectedFileCounts == nil {
					return ValidationPlan{}, fmt.Errorf("Component %q impact is missing affectedFileCounts", component.Root)
				}
				for _, count := range impact.AffectedFileCounts {
					if count < 0 {
						return ValidationPlan{}, errors.New("impact counts must be non-negative")
					}
				}
				relevant := false
				for _, tag := range tags {
					if impact.AffectedFileCounts[cg.Tag(tag)] > 0 {
						relevant = true
					}
				}
				if !relevant {
					spec.Scope = "skipped"
					spec.Reason = "no selected validation tags"
					group.Checks = append(group.Checks, spec)
					continue
				}
				candidates = impact.AffectedFiles
				if check == "test" {
					candidates = impact.TestFiles
				}
				spec.Reason = "affected Component; canonical full check"
			}
			target := contract.target(check)
			if target == "" {
				spec.Scope = "unavailable"
				spec.Reason = "Makefile does not declare a supported " + check + " target"
				if contract.problem != "" {
					spec.Reason = contract.problem
				}
				group.Checks = append(group.Checks, spec)
				continue
			}
			variable := strings.ToUpper(check) + "_FILES"
			files, reason := validationFiles(plan.Checkout, component.Root, layout.Layout, candidates)
			if len(candidates) > 0 {
				if reason != "" {
					spec.Reason = reason + "; canonical full check"
				} else if !contract.variables[variable] {
					spec.Reason = variable + " contract missing; canonical full check"
				} else {
					spec.Scope = "focused"
					spec.Files = files
					spec.Reason = "selected files under Makefile " + variable + " contract"
				}
			}
			spec.Command = []string{"make", "-f", "Makefile", target, "LINT_FILES=", "TEST_FILES="}
			if spec.Scope == "focused" {
				idx := 4
				if check == "test" {
					idx = 5
				}
				spec.Command[idx] = variable + "=" + strings.Join(spec.Files, " ")
			}
			group.Checks = append(group.Checks, spec)
		}
		plan.Components = append(plan.Components, group)
	}
	for root, found := range selected {
		if !found {
			return ValidationPlan{}, fmt.Errorf("unknown Component %q", root)
		}
	}
	plan.seal = planSeal(plan)
	return plan, nil
}

func validationFiles(root, component string, layout Layout, candidates []string) ([]string, string) {
	files := []string{}
	for _, path := range candidates {
		if filepath.IsAbs(path) || filepath.ToSlash(filepath.Clean(path)) != path || path == ".." || strings.HasPrefix(path, "../") {
			return nil, "selected path is not normalized and repository-relative"
		}
		owner := layout.Owner(path)
		if owner == nil || owner.Root != component {
			return nil, "selected path no longer belongs to Component"
		}
		absolute := filepath.Join(root, filepath.FromSlash(path))
		info, err := os.Lstat(absolute)
		if err != nil || !info.Mode().IsRegular() {
			return nil, "selected path is missing or not a regular file"
		}
		real, err := filepath.EvalSymlinks(absolute)
		if err != nil || real != absolute {
			return nil, "selected path crosses a symlink"
		}
		relative, err := filepath.Rel(filepath.Join(root, component), absolute)
		if err != nil || !makeFilePath.MatchString(relative) {
			return nil, "selected path cannot be represented by Make file-list contract"
		}
		files = append(files, filepath.ToSlash(relative))
	}
	sort.Strings(files)
	return slices.Compact(files), ""
}

func planSeal(plan ValidationPlan) string {
	encoded, _ := json.Marshal(plan)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// ExecuteValidation consumes only the frozen plan. Two round-level captures,
// independent of the number of Components/checks, guard all returned evidence.
// +spec=Planning and final consistency failures never publish a complete validation report
// +spec=The executor never recalculates the scope of a frozen command
// +link=component://docs/validation.md
func ExecuteValidation(ctx context.Context, plan ValidationPlan) (ValidationReport, error) {
	report := ValidationReport{Checkout: plan.Checkout, Snapshot: plan.Snapshot, Components: []ComponentValidationResult{}, Diagnostics: []string{}}
	if plan.seal == "" || plan.seal != planSeal(plan) {
		return report, errors.New("validation plan was modified or deserialized; plan again")
	}
	before, err := Snapshot(ctx, InputRequest{Repository: plan.Checkout})
	if err != nil {
		return report, err
	}
	if !before.Complete || before.Snapshot != plan.Snapshot {
		report.Diagnostics = append(report.Diagnostics, "contents changed after planning; no checks executed")
		return report, nil
	}
	report.Complete = true
	report.Passed = true
	for _, component := range plan.Components {
		group := ComponentValidationResult{Component: component.Component, Checks: []CheckResult{}}
		for _, spec := range component.Checks {
			result := CheckResult{CheckSpec: spec, Status: spec.Scope}
			if spec.Scope == "skipped" {
				group.Checks = append(group.Checks, result)
				continue
			}
			if spec.Scope == "unavailable" {
				report.Complete = false
				report.Passed = false
				group.Checks = append(group.Checks, result)
				continue
			}
			started := time.Now()
			result = executeMake(ctx, filepath.Join(plan.Checkout, component.Component.Root), spec, plan.environment)
			result.DurationSeconds = time.Since(started).Seconds()
			if result.Status != "passed" {
				report.Passed = false
			}
			if result.Status != "passed" && result.Status != "failed" {
				report.Complete = false
			}
			group.Checks = append(group.Checks, result)
		}
		report.Components = append(report.Components, group)
	}
	after, err := Snapshot(ctx, InputRequest{Repository: plan.Checkout})
	if err != nil || !after.Complete || after.Snapshot != plan.Snapshot {
		report.Complete = false
		report.Passed = false
		message := "contents changed during validation"
		if err != nil {
			message = "final content capture failed: " + err.Error()
		}
		report.Diagnostics = append(report.Diagnostics, message)
	}
	return report, nil
}

// Validate plans and executes lint/test. Lint and Test use the same planner and executor.
func Validate(ctx context.Context, req ValidationRequest) (ValidationReport, error) {
	plan, err := PlanValidation(ctx, req)
	if err != nil {
		return ValidationReport{}, err
	}
	return ExecuteValidation(ctx, plan)
}
func Lint(ctx context.Context, req ValidationRequest) (ValidationReport, error) {
	req.Checks = []string{"lint"}
	return Validate(ctx, req)
}
func Test(ctx context.Context, req ValidationRequest) (ValidationReport, error) {
	req.Checks = []string{"test"}
	return Validate(ctx, req)
}
