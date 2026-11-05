package validation

import (
	"crypto/sha256"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	cg "github.com/compforge/codegraph"
	"github.com/compforge/repokit/toolkit/go/internal/analysis"
	"github.com/compforge/repokit/toolkit/go/internal/project"
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

// inputEntry records content, not source text. Version entries remain in the
// complete manifest and are ignored only when comparing reusable evidence.
type inputEntry struct {
	Kind    string `json:"kind"`
	Digest  string `json:"digest"`
	Version bool   `json:"version,omitempty"`
}
type inputManifest struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Checkout      string                `json:"checkout"`
	Snapshot      string                `json:"snapshot"`
	Files         map[string]inputEntry `json:"files"`
}

// +spec=One round hashes each captured file once, independently of check count
// +link=component://docs/validation.md
func captureInputs(captured *analysis.CapturedSnapshot) inputManifest {
	m := inputManifest{SchemaVersion: 1, Checkout: captured.Report.Checkout, Snapshot: captured.Report.Snapshot, Files: map[string]inputEntry{}}
	put := func(path, kind string, data []byte) {
		m.Files[path] = inputEntry{Kind: kind, Digest: fmt.Sprintf("%x", sha256.Sum256(data)), Version: slices.Contains(project.FileTags(path, captured.Contents.Modules[path]), cg.VersionTag)}
	}
	for path, data := range captured.Contents.Files {
		put(path, "file", data)
	}
	for kind, entries := range map[string]map[string]string{"symlink": captured.Contents.Links, "gitlink": captured.Contents.Modules, "opaque": captured.Contents.Opaque} {
		for path, value := range entries {
			put(path, kind, []byte(value))
		}
	}
	return m
}

// Validate declarations without filtering or hashing repository content.
func validateDeclaration(checkout string, layout project.Layout, component string, d CheckInputDeclaration) (map[string]string, error) {
	if d.Components != nil || d.Paths != nil {
		for _, root := range append(slices.Clone(d.Components), component) {
			if !slices.ContainsFunc(layout.Components, func(c project.Binding) bool { return c.Root == root }) {
				return nil, fmt.Errorf("unknown input Component %q", root)
			}
		}
		for _, path := range d.Paths {
			if path == "" || filepath.IsAbs(path) || filepath.ToSlash(filepath.Clean(path)) != path || path == ".." || strings.HasPrefix(path, "../") {
				return nil, fmt.Errorf("input path %q must be normalized and repository-relative", path)
			}
		}
	}
	witnesses := map[string]string{}
	for _, path := range d.Witnesses {
		absolute := path
		if !filepath.IsAbs(absolute) {
			absolute = filepath.Join(checkout, path)
		}
		value, err := validationWitness(absolute)
		if err != nil {
			return nil, fmt.Errorf("input witness %q: %w", path, err)
		}
		witnesses[absolute] = value
	}
	return witnesses, nil
}

func selectInputs(m inputManifest, layout project.Layout, component string, d CheckInputDeclaration) ([]string, string) {
	all, selected := []string{}, []string{}
	declared := d.Components != nil || d.Paths != nil
	links := false
	for path, entry := range m.Files {
		all = append(all, path)
		owner := layout.Owner(path)
		belongs := owner != nil && (owner.Root == component || slices.Contains(d.Components, owner.Root))
		if declared && (belongs || slices.ContainsFunc(d.Paths, func(root string) bool { return root == "." || path == root || strings.HasPrefix(path, root+"/") })) {
			selected = append(selected, path)
			links = links || entry.Kind == "symlink"
		}
	}
	reason := "declared Component and shared-path inputs"
	if !declared || links {
		selected = all
		reason = "repository inputs: no complete declaration"
		if links {
			reason = "repository inputs: declared scope contains symlinks"
		}
	}
	slices.Sort(selected)
	return selected, reason
}

// Compare both path sets so additions, removals and scope changes invalidate runs.
// +spec=VersionTag changes between rounds do not invalidate check history
func sameInputs(before inputManifest, previous []string, after inputManifest, current []string) bool {
	entries := func(m inputManifest, paths []string) (map[string]inputEntry, bool) {
		result := map[string]inputEntry{}
		for _, path := range paths {
			e, ok := m.Files[path]
			if !ok {
				return nil, false
			}
			if !e.Version {
				result[path] = e
			}
		}
		return result, true
	}
	a, okA := entries(before, previous)
	b, okB := entries(after, current)
	return okA && okB && maps.Equal(a, b)
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
	if !depotWitnessesMatch(plan) {
		return false
	}
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
		if _, err := validateDeclaration(captured.Report.Checkout, layout, source, sourceInputs); err != nil {
			return CheckInputDeclaration{}, fmt.Errorf("%s %s inputs: %w", source, spec.Check, err)
		}
		declaration.Witnesses = append(slices.Clone(declaration.Witnesses), sourceInputs.Witnesses...)
		declaration.DisableReuse = declaration.DisableReuse || sourceInputs.DisableReuse
	}
	if fallback {
		if _, err := validateDeclaration(captured.Report.Checkout, layout, executor, inputs[executor][spec.Check]); err != nil {
			return CheckInputDeclaration{}, fmt.Errorf("%s %s inputs: %w", executor, spec.Check, err)
		}
		declaration.Components, declaration.Paths = nil, nil
		slices.Sort(declaration.Witnesses)
		declaration.Witnesses = slices.Compact(declaration.Witnesses)
	}
	return declaration, nil
}
