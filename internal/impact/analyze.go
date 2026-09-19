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
	Version        string               `json:"version,omitempty"` // Snapshot owning the dependency evidence.
	TestFile       string               `json:"testFile"`
	Kind           string               `json:"kind"`
	DependencyPath []string             `json:"dependencyPath,omitempty"`
	Relations      []codegraph.Relation `json:"relations,omitempty"`
}

type Result struct {
	SchemaVersion   int           `json:"schemaVersion"`
	Changes         []FileChange  `json:"changes"`
	Scope           string        `json:"scope"`
	TestFiles       []string      `json:"testFiles"`
	SourceFiles     []string      `json:"sourceFiles"`
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
	TestPatterns                    []string
	Mode                            string
	Issues                          []string
	Skipped                         map[string]string
	Gitlinks                        map[string]bool
	OldLayout, NewLayout            project.Layout
}

func Analyze(ctx context.Context, req Request) (Result, error) {
	patterns, err := ValidateTestPatterns(req.TestPatterns)
	if err != nil {
		return Result{}, err
	}
	req.TestPatterns = patterns
	if len(req.TestDirs) == 0 && len(patterns) > 0 {
		req.TestDirs = []string{"."}
	}
	r := Result{SchemaVersion: 2, Scope: "not_requested", Changes: []FileChange{}, TestFiles: []string{}, SourceFiles: []string{}, Reasons: []Reason{}, FallbackReasons: []string{}}
	a := &codegraph.Analyzer{}
	for _, c := range req.Changes {
		if codegraph.Language(c.Path) != "" {
			r.SourceFiles = append(r.SourceFiles, c.Path)
		}
		oldName := c.Path
		if c.OldPath != "" {
			oldName = c.OldPath
		}
		before := a.Source(ctx, oldName, req.Before[oldName])
		after := a.Source(ctx, c.Path, req.After[c.Path])
		if len(req.TestDirs) == 0 {
			for _, issue := range append(before.Issues, after.Issues...) {
				r.FallbackReasons = append(r.FallbackReasons, c.Path+": "+issue.Message)
			}
		}
		r.Changes = append(r.Changes, FileChange{Change: c, Before: changedSymbols(before.Symbols, c.Hunks, true), After: changedSymbols(after.Symbols, c.Hunks, false)})
	}
	r.SourceFiles = unique(r.SourceFiles)
	r.FallbackReasons = append(r.FallbackReasons, req.Issues...)
	if len(req.TestDirs) == 0 {
		for _, issue := range skippedGaps(req) {
			if issue.path != "" {
				r.FallbackReasons = append(r.FallbackReasons, issue.path+": "+issue.message)
			}
		}
		r.FallbackReasons = unique(r.FallbackReasons)
		return r, ctx.Err()
	}
	// Test directories constrain candidate discovery, not dependency semantics.
	// A root such as "." or "src" may contain production code and test helpers;
	// analyze their imports normally. Known implicit hooks are broadChange inputs.
	var tests []string
	for name := range req.After {
		if within(name, req.TestDirs) && isTest(name, req.TestPatterns) {
			tests = append(tests, name)
		}
	}
	sort.Strings(tests)
	r.Scope = "focused"
	if len(req.Changes) == 0 && len(skippedGaps(req)) == 0 {
		return r, ctx.Err()
	}
	var graphs []*codegraph.Graph
	var builds []codegraph.BuildResult
	roots := []string{}
	for _, change := range req.Changes {
		roots = append(roots, change.Path)
		if change.OldPath != "" {
			roots = append(roots, change.OldPath)
		}
	}
	gaps := skippedGaps(req)
	if len(tests) == 0 {
		gaps = append(gaps, gap{reason: "no_candidates", message: "no supported test files matched the requested directories and patterns", global: true})
	}
	r.FallbackReasons = []string{}
	kinds := []codegraph.Kind{codegraph.Imports, codegraph.Reexports, codegraph.PackageMember, codegraph.ConfigExtends, codegraph.ConfigScope}
	symbolFiles := []string{}
	if req.Mode != "file" {
		kinds = append(kinds, codegraph.Contains, codegraph.Calls)
		symbolFiles = roots
	}
	var seeds []string
	for _, c := range req.Changes {
		if req.Mode == "file" {
			seeds = append(seeds, c.Path)
		} else {
			seeds = append(seeds, changeSeeds(c, a.Source(ctx, c.Path, req.Before[c.Path]), a.Source(ctx, c.Path, req.After[c.Path]), req.TestPatterns)...)
		}
		if c.OldPath != "" {
			seeds = append(seeds, c.OldPath)
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
	for index, snapshot := range []map[string][]byte{req.Before, req.After} {
		resources := req.BeforeResources
		if index == 1 {
			resources = req.AfterResources
		}
		builder, err := codegraph.NewBuilder(codegraph.BuildOptions{Files: snapshot,
			Gitlinks: req.Gitlinks, Resources: resources, SymbolFiles: symbolFiles, Kinds: kinds,
			MaxDepth: 32, MaxFiles: 2000, Analyzer: a})
		if err != nil {
			return r, err
		}
		// Scope each snapshot independently: an import removed by this diff still
		// makes its former consumer relevant on the before side.
		candidates, err := builder.ScopeCandidates(ctx, roots, tests)
		if err != nil {
			return r, err
		}
		for _, test := range candidates {
			if err := builder.Add(ctx, test); err != nil {
				return r, err
			}
		}
		built := builder.Result()
		graphs = append(graphs, built.Graph)
		builds = append(builds, built)
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
	r.selectTests(graphs, builds, req, tests, seeds, gaps)
	return r, ctx.Err()
}

func changeSeeds(c diff.Change, before, after codegraph.Source, patterns []string) []string {
	// Named imports permit a deliberately coarse symbol heuristic: importing a
	// changed declaration is sufficient; its actual use inside tests isn't checked.
	// Go imports packages, and module-level edits have no narrower symbol contract.
	if c.Status != "modified" || isTest(c.Path, patterns) || before.Language == "go" || len(c.Hunks) == 0 {
		return []string{c.Path}
	}
	var symbols []string
	for _, h := range c.Hunks {
		for _, side := range []struct {
			r     diff.Range
			facts codegraph.Source
		}{{h.Old, before}, {h.New, after}} {
			if side.r.Count == 0 {
				continue
			}
			covered := false
			for _, s := range side.facts.Symbols {
				if side.r.Start >= s.StartLine && side.r.Start+side.r.Count-1 <= s.EndLine {
					covered = true
					symbols = append(symbols, codegraph.SymbolID(c.Path, s.QualifiedName))
				}
			}
			if !covered {
				return []string{c.Path}
			}
		}
	}
	if len(symbols) == 0 {
		return []string{c.Path}
	}
	return append(unique(symbols), codegraph.ModuleID(c.Path))
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
