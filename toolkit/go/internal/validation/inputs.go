package validation

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/compforge/repocli/toolkit/go/internal/analysis"
	"github.com/compforge/repocli/toolkit/go/internal/git"
	"github.com/compforge/repocli/toolkit/go/internal/project"
)

// CheckInputDeclaration declares complete transitive inputs, independently of
// affected-file selection. Nil Components/Paths conservatively uses the repository.
// Witnesses are caller-selected external/ignored receipt files, never their contents.
// DisableReuse permits real checks in environments without trustworthy receipts.
type CheckInputDeclaration struct {
	Components   []string `json:"components,omitempty"`
	Paths        []string `json:"paths,omitempty"`
	Witnesses    []string `json:"witnesses,omitempty"`
	DisableReuse bool     `json:"disableReuse,omitempty"`
}

func validationFingerprint(captured *analysis.CapturedSnapshot, layout project.Layout, component string, declaration CheckInputDeclaration) (string, string, map[string]string, error) {
	contents := captured.Contents
	digest, reason := captured.Report.Snapshot, "repository inputs: no complete declaration"
	if declaration.Components != nil || declaration.Paths != nil {
		components := append(slices.Clone(declaration.Components), component)
		for _, root := range components {
			if !slices.ContainsFunc(layout.Components, func(c project.Binding) bool { return c.Root == root }) {
				return "", "", nil, fmt.Errorf("unknown input Component %q", root)
			}
		}
		for _, path := range declaration.Paths {
			if path == "" || filepath.IsAbs(path) || filepath.ToSlash(filepath.Clean(path)) != path || path == ".." || strings.HasPrefix(path, "../") {
				return "", "", nil, fmt.Errorf("input path %q must be normalized and repository-relative", path)
			}
		}
		selected := func(path string) bool {
			if owner := layout.Owner(path); owner != nil && slices.Contains(components, owner.Root) {
				return true
			}
			return slices.ContainsFunc(declaration.Paths, func(root string) bool { return root == "." || path == root || strings.HasPrefix(path, root+"/") })
		}
		subset := git.Snapshot{Files: map[string][]byte{}, Links: map[string]string{}, Modules: map[string]string{}, Opaque: map[string]string{}}
		for path, value := range contents.Files {
			if selected(path) {
				subset.Files[path] = value
			}
		}
		for path, value := range contents.Links {
			if selected(path) {
				subset.Links[path] = value
			}
		}
		for path, value := range contents.Modules {
			if selected(path) {
				subset.Modules[path] = value
			}
		}
		for path, value := range contents.Opaque {
			if selected(path) {
				subset.Opaque[path] = value
			}
		}
		if len(subset.Links) == 0 {
			digest, reason = subset.Digest(), "declared Component and shared-path inputs"
		} else {
			reason = "repository inputs: declared scope contains symlinks"
		}
	}
	witnesses := map[string]string{}
	for _, path := range declaration.Witnesses {
		absolute := path
		if !filepath.IsAbs(absolute) {
			absolute = filepath.Join(captured.Report.Checkout, path)
		}
		value, err := validationWitness(absolute)
		if err != nil {
			return "", "", nil, fmt.Errorf("input witness %q: %w", path, err)
		}
		witnesses[absolute] = value
	}
	if declaration.DisableReuse {
		return "", "reuse disabled by caller", witnesses, nil
	}
	encoded, _ := json.Marshal(struct {
		Digest      string
		Declaration CheckInputDeclaration
		Witnesses   map[string]string
	}{digest, declaration, witnesses})
	return fmt.Sprintf("sha256:%x", sha256.Sum256(encoded)), reason, witnesses, nil
}

func validationWitness(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 2<<20 {
		return "", fmt.Errorf("expected regular receipt of at most 2 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(file, (2<<20)+1))
	if err != nil {
		return "", err
	}
	if len(data) > 2<<20 {
		return "", fmt.Errorf("receipt exceeded 2 MiB")
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

func validationWitnessesMatch(plan ValidationPlan) bool {
	observed := map[string]string{}
	for _, group := range plan.Components {
		for _, spec := range group.Checks {
			for path, expected := range spec.Witnesses {
				actual, found := observed[path]
				if !found {
					var err error
					actual, err = validationWitness(path)
					if err != nil {
						return false
					}
					observed[path] = actual
				}
				if actual != expected {
					return false
				}
			}
		}
	}
	return true
}

// +spec=Reuse requires successful evidence for identical check inputs, argv, scope and execution environment
// +spec=Fingerprints never select checks or widen their affected-file scope
func reusableValidation(plan ValidationPlan, root string, spec CheckSpec) (*CheckResult, string) {
	if spec.InputFingerprint == "" {
		return nil, spec.InputReason
	}
	reason := "no previous successful check"
	for _, report := range plan.previous {
		if report.SchemaVersion != 1 || report.Checkout != plan.Checkout || !report.InputsConsistent {
			continue
		}
		for _, group := range report.Components {
			if group.Component.Root == root {
				for _, previous := range group.Checks {
					if previous.Check != spec.Check || previous.Status != "passed" || previous.CheckedAt.IsZero() {
						continue
					}
					if previous.InputFingerprint != spec.InputFingerprint {
						reason = "check inputs changed"
						continue
					}
					if previous.Environment != spec.Environment {
						reason = "execution environment changed"
						continue
					}
					if previous.Scope != spec.Scope || !slices.Equal(previous.Command, spec.Command) || !slices.Equal(previous.Files, spec.Files) || !reflect.DeepEqual(previous.Witnesses, spec.Witnesses) {
						reason = "command, scope or receipt changed"
						continue
					}
					return &previous, "identical check inputs, command, scope and execution environment"
				}
			}
		}
	}
	return nil, reason
}

// Root fallback may cover nested Components excluded by a root-only declaration.
// Bind it conservatively to the repository while retaining every source receipt
// and reuse prohibition. Validate source declarations before widening the scope.
func validationCheckInputs(captured *analysis.CapturedSnapshot, layout project.Layout, executor string, spec CheckSpec, inputs map[string]map[string]CheckInputDeclaration) (CheckInputDeclaration, error) {
	declaration := inputs[executor][spec.Check]
	fallback := false
	for _, source := range spec.RequestedComponents {
		if source == executor {
			continue
		}
		fallback = true
		sourceInputs := inputs[source][spec.Check]
		if _, _, _, err := validationFingerprint(captured, layout, source, sourceInputs); err != nil {
			return CheckInputDeclaration{}, fmt.Errorf("%s %s inputs: %w", source, spec.Check, err)
		}
		declaration.Witnesses = append(slices.Clone(declaration.Witnesses), sourceInputs.Witnesses...)
		declaration.DisableReuse = declaration.DisableReuse || sourceInputs.DisableReuse
	}
	if fallback {
		if _, _, _, err := validationFingerprint(captured, layout, executor, inputs[executor][spec.Check]); err != nil {
			return CheckInputDeclaration{}, fmt.Errorf("%s %s inputs: %w", executor, spec.Check, err)
		}
		declaration.Components, declaration.Paths = nil, nil
		slices.Sort(declaration.Witnesses)
		declaration.Witnesses = slices.Compact(declaration.Witnesses)
	}
	return declaration, nil
}
