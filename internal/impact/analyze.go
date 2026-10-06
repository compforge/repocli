// Package impact selects tests through separately queried before/after code graphs.
package impact

import (
	"bytes"
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/compforge/repocli/internal/codegraph"
	"github.com/compforge/repocli/internal/diff"
	"github.com/compforge/repocli/internal/project"
)

type FileChange struct {
	diff.Change
	Before []codegraph.Symbol `json:"beforeSymbols"`
	After  []codegraph.Symbol `json:"afterSymbols"`
}

type Reason struct {
	Seed           Seed                 `json:"seed"`
	Confidence     codegraph.Confidence `json:"confidence"`
	Distance       int                  `json:"distance"`
	Version        string               `json:"version,omitempty"` // Snapshot owning the dependency evidence.
	TestFile       string               `json:"testFile"`
	Kind           string               `json:"kind"`
	DependencyPath []string             `json:"dependencyPath,omitempty"`
	Relations      []codegraph.Relation `json:"relations,omitempty"`
}

type FileImpact struct {
	Path           string               `json:"path"`
	Version        string               `json:"version"`
	Seed           Seed                 `json:"seed"`
	Confidence     codegraph.Confidence `json:"confidence"`
	Distance       int                  `json:"distance"`
	DependencyPath []string             `json:"dependencyPath"`
	Relations      []codegraph.Relation `json:"relations"`
}

type Result struct {
	SchemaVersion   int           `json:"schemaVersion"`
	Changes         []FileChange  `json:"changes"`
	Scope           string        `json:"scope"`
	TestFiles       []string      `json:"testFiles"`
	SourceFiles     []string      `json:"sourceFiles"`
	AffectedFiles   []FileImpact  `json:"affectedFiles"`
	Seeds           []Seed        `json:"seeds"`
	Reasons         []Reason      `json:"reasons"`
	FallbackReasons []string      `json:"fallbackReasons"` // Legacy wire name for incompleteness reasons; no fallback tests are synthesized.
	Uncertainties   []Uncertainty `json:"-"`
	Observations    []Uncertainty `json:"observations,omitempty"`
}

type Request struct {
	Before, After                   map[string][]byte
	BeforeResources, AfterResources map[string][]byte
	Changes                         []diff.Change
	TestDirs                        []string
	Issues                          []string
	Skipped                         map[string]string
	Gitlinks                        map[string]bool
	OldLayout, NewLayout            project.Layout
}

func Analyze(ctx context.Context, req Request) (Result, error) {
	r := Result{SchemaVersion: 3, Scope: "focused", Changes: []FileChange{}, TestFiles: []string{}, SourceFiles: []string{}, AffectedFiles: []FileImpact{}, Seeds: []Seed{}, Reasons: []Reason{}, FallbackReasons: []string{}}
	if len(req.Changes) == 0 && len(skippedGaps(req)) == 0 {
		return r, ctx.Err()
	}
	// Test discovery selects query candidates, not graph boundaries. Dependencies
	// outside these directories can still be necessary intermediate documents.
	tests := []string{}
	for name := range req.After {
		if within(name, req.TestDirs) && IsTest(name) {
			tests = append(tests, name)
		}
	}
	sort.Strings(tests)
	builds, err := buildWorksets(ctx, req, tests)
	if err != nil {
		return r, err
	}
	for _, c := range req.Changes {
		if codegraph.Language(c.Path) != "" {
			r.SourceFiles = append(r.SourceFiles, c.Path)
		}
		oldName := c.Path
		if c.OldPath != "" {
			oldName = c.OldPath
		}
		before := builds[0].Sources[oldName]
		after := builds[1].Sources[c.Path]
		r.Changes = append(r.Changes, FileChange{Change: c, Before: changedSymbols(before.Symbols, c.Hunks, true), After: changedSymbols(after.Symbols, c.Hunks, false)})
	}
	r.SourceFiles = unique(r.SourceFiles)
	graphs := []*codegraph.Graph{builds[0].Graph, builds[1].Graph}
	gaps := skippedGaps(req)
	if len(req.TestDirs) != 0 && len(tests) == 0 {
		gaps = append(gaps, gap{reason: "no_candidates", message: "no supported test filenames found under the requested test directories", global: true})
	}
	for _, c := range req.Changes {
		oldName := c.Path
		if c.OldPath != "" {
			oldName = c.OldPath
		}
		if _, exists := req.Before[oldName]; exists || req.Gitlinks[oldName] {
			r.Seeds = append(r.Seeds, selectSeeds(c, builds[0].Sources[oldName], true)...)
		}
		if _, exists := req.After[c.Path]; exists || req.Gitlinks[c.Path] {
			r.Seeds = append(r.Seeds, selectSeeds(c, builds[1].Sources[c.Path], false)...)
		}
		for _, name := range []string{c.Path, c.OldPath} {
			if name == "" || req.Gitlinks[name] {
				continue
			}
			if observation, ok := metadataChange(name, req.Before[name], req.After[name]); ok {
				r.Observations = append(r.Observations, observation)
				continue
			}
			if ignoredDependency(name) {
				gaps = append(gaps, gap{reason: "source_boundary", path: name, message: "vendored or environment dependencies are not indexed", global: true})
			} else if broadChange(name) {
				gaps = append(gaps, gap{reason: "configuration_change", path: name, message: "build, dependency, or test configuration changed", component: true})

			} else if codegraph.Language(name) == "" {
				gaps = append(gaps, gap{reason: "unsupported_resource_change", path: name, message: "non-source dependency impact is not modeled", component: true})
			}
		}
	}
	// Reading a child config does not import child sources. A consumed resource
	// changing across snapshots is nevertheless a parent resolution input change.
	resources := map[string]bool{}
	for name := range req.BeforeResources {
		resources[name] = true
	}
	for name := range req.AfterResources {
		resources[name] = true
	}
	for name := range resources {
		if bytes.Equal(req.BeforeResources[name], req.AfterResources[name]) {
			continue
		}
		for _, g := range graphs {
			consumed := false
			for _, edge := range g.Incoming(name) {
				if edge.Kind == codegraph.ConfigExtends {
					consumed = true
				}
			}
			if consumed {
				gaps = append(gaps, gap{path: name, reason: "configuration_change", message: "inherited configuration resource changed"})
				break
			}
		}
	}
	if err := r.selectTests(ctx, graphs, builds, req, tests, gaps); err != nil {
		return r, err
	}
	return r, ctx.Err()
}

func changedSymbols(symbols []codegraph.Symbol, hunks []diff.Hunk, old bool) []codegraph.Symbol {
	out := []codegraph.Symbol{}
	for _, s := range symbols {
		for _, h := range hunks {
			r := h.New
			if old {
				r = h.Old
			}
			// Only actual changed lines select symbols; insertion anchors are not
			// edits to the neighbouring declaration on the opposite snapshot.
			if r.Count > 0 && r.Start <= s.EndLine && r.Start+r.Count-1 >= s.StartLine {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

func IsTest(name string) bool {
	b := path.Base(name)
	switch codegraph.Language(name) {
	case "go":
		return strings.HasSuffix(b, "_test.go")
	case "python":
		return strings.HasPrefix(b, "test_") && strings.HasSuffix(b, ".py") || strings.HasSuffix(b, "_test.py")
	case "typescript", "tsx", "javascript":
		return strings.Contains(b, ".test.") || strings.Contains(b, ".spec.") || strings.Contains("/"+name, "/__tests__/")
	}
	return false
}

func within(name string, dirs []string) bool {
	for _, dir := range dirs {
		if dir == "." || name == dir || strings.HasPrefix(name, dir+"/") {
			return true
		}
	}
	return false
}

func broadChange(name string) bool {
	b := path.Base(name)
	switch b {
	case "go.mod", "go.sum", "go.work", "go.work.sum", "package.json", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "pnpm-workspace.yaml", "bun.lock", "bun.lockb", "pyproject.toml", "uv.lock", "poetry.lock", "Pipfile", "Pipfile.lock", "setup.py", "setup.cfg", "tox.ini", "pytest.ini", "conftest.py", "Makefile":
		return true
	}
	return strings.HasPrefix(b, "tsconfig") || strings.HasPrefix(b, "requirements") || strings.Contains(b, ".config.") || strings.HasPrefix(b, ".env")
}

func unique(values []string) []string {
	sort.Strings(values)
	out := make([]string, 0, len(values))
	for _, v := range values {
		if len(out) == 0 || out[len(out)-1] != v {
			out = append(out, v)
		}
	}
	return out
}

func ValidateDirs(dirs []string) ([]string, error) {
	out := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if dir == "" || strings.HasPrefix(dir, "/") {
			return nil, fmt.Errorf("test directory must be relative to repository root: %q", dir)
		}
		dir = strings.TrimSuffix(dir, "/")
		dir = strings.TrimPrefix(dir, "./")
		if dir == "" {
			dir = "."
		}
		if dir != "." && !diff.ValidPath(dir) {
			return nil, fmt.Errorf("test directory must be relative to repository root: %q", dir)
		}
		out = append(out, dir)
	}
	return unique(out), nil
}
