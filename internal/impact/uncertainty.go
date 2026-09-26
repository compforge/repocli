package impact

import (
	"context"
	"sort"
	"strings"
	"time"

	shared "github.com/compforge/codegraph"
	"github.com/compforge/go-stdx/timeline"
	"github.com/compforge/repocli/internal/codegraph"
	"github.com/compforge/repocli/internal/project"
)

type gap struct {
	path, message, reason string
	version               string
	relation              codegraph.Kind
	line                  int
	component             bool
	global                bool
}

// Uncertainty records actual analysis gaps and their reporting scope.
type Uncertainty struct {
	Subject         shared.DiagnosticSubject `json:"subject,omitempty"`
	Location        shared.Location          `json:"location,omitzero"`
	Outline         *shared.OutlineCoverage  `json:"outline,omitempty"`
	Reason          string                   `json:"reason,omitempty"`
	Relation        codegraph.Kind           `json:"relation,omitempty"`
	Confidence      codegraph.Confidence     `json:"confidence,omitempty"`
	Version         string                   `json:"version,omitempty"`
	Line            int                      `json:"line,omitempty"`
	PossibleTargets []string                 `json:"possibleTargets,omitempty"`
	Disposition     string                   `json:"disposition,omitempty"`
	Path            string                   `json:"path,omitempty"`
	Message         string                   `json:"message"`
	Scope           string                   `json:"scope"`
	TestFiles       []string                 `json:"-"`
}

func boundGap(graphs []*codegraph.Graph, issue gap, req Request, tests []string) Uncertainty {
	u := Uncertainty{Reason: issue.reason, Path: issue.path, Message: issue.message, Version: issue.version, Relation: issue.relation, Line: issue.line, Scope: "dependency", TestFiles: []string{}}
	var seeds []string
	if issue.path != "" {
		seeds = append(seeds, issue.path)
	}
	// A malformed changed source or an implicit config/resource can affect any
	// file owned by that component. This bounds diagnostics, not test selection.
	expand := issue.component
	for _, change := range req.Changes {
		if change.Path == issue.path || change.OldPath == issue.path {
			expand = true
		}
	}
	if expand && !issue.global {
		u.Scope = "component"
		for _, side := range []struct {
			layout project.Layout
			files  map[string][]byte
		}{{req.OldLayout, req.Before}, {req.NewLayout, req.After}} {
			owner := side.layout.Owner(issue.path)
			if owner == nil || owner.Root == "." {
				issue.global = true
				break
			}
			for name := range side.files {
				if binding := side.layout.Owner(name); binding != nil && binding.Root == owner.Root {
					seeds = append(seeds, name)
				}
			}
		}
	}
	if issue.global {
		u.Scope = "repository"
		u.TestFiles = append(u.TestFiles, tests...)
		return u
	}
	routes := impactPaths(graphs, seeds, true)
	for _, test := range tests {
		if _, ok := routes[test]; ok {
			u.TestFiles = append(u.TestFiles, test)
		}
	}
	return u
}

func (r *Result) selectTests(ctx context.Context, graphs []*codegraph.Graph, builds []codegraph.BuildResult, req Request, tests []string, gaps []gap) error {
	routes := map[string]impactPath{}
	for index, built := range builds {
		version := "before"
		if index == 1 {
			version = "after"
		}
		var seeds []string
		provenance := map[string]Seed{}
		for _, seed := range r.Seeds {
			if seed.Version == version {
				seeds = append(seeds, seed.ID)
				provenance[seed.ID] = seed
			}
		}
		candidates := []string{}
		for id, node := range built.Graph.Nodes {
			if node.Kind == "file" {
				if _, exists := req.After[id]; exists {
					candidates = append(candidates, id)
				}
			}
		}
		sort.Strings(candidates)
		started := time.Now()
		query, err := built.Query(ctx, seeds, candidates, impactKinds)
		if operation, ok := timeline.FromContext(ctx); ok {
			operation.StepSince(started, "query."+version,
				timeline.Field{Key: "nodes", Value: len(built.Graph.Nodes)},
				timeline.Field{Key: "diagnostics", Value: len(built.Diagnostics)},
				timeline.Field{Key: "candidates", Value: len(candidates)},
				timeline.Field{Key: "failed", Value: err != nil})
		}
		if err != nil {
			return err
		}
		for id, route := range query.Paths {
			if old, ok := routes[id]; !ok || codegraph.ComparePaths(route, old.Path) < 0 {
				routes[id] = impactPath{Path: route, Version: version, Seed: provenance[route.Nodes[len(route.Nodes)-1]]}
			}
		}
		for _, diagnostic := range built.Diagnostics {
			if diagnostic.Subject == shared.DocumentSubject || diagnostic.Subject == shared.DeclarationsSubject || diagnostic.Subject == shared.RelationsSubject {
				// Extraction gaps describe missing local facts. They do not prove
				// every candidate test is uncertain; declaration gaps already widen
				// changed seeds. Keep the full local evidence available to callers.
				r.Observations = append(r.Observations, Uncertainty{
					Path: diagnostic.Path, Message: diagnostic.Message, Reason: diagnostic.Code,
					Relation: diagnostic.Kind, Line: diagnostic.Line, Version: version,
					Subject: diagnostic.Subject, Location: diagnostic.Location, Outline: diagnostic.Outline,
					Scope: "document", Disposition: "local_gap",
				})
				continue
			}
			// Repository configuration and exhausted budgets are consumer-owned
			// limits. Their scope is evaluated separately from parser coverage.
			gaps = append(gaps, gap{path: diagnostic.Path, reason: diagnostic.Code, message: diagnostic.Message,
				version: version, relation: diagnostic.Kind, line: diagnostic.Line,
				global:    diagnostic.Code == "expansion_limit" || diagnostic.Subject == shared.ContextSubject || diagnostic.Subject == shared.ResourcesSubject,
				component: diagnostic.Code == "boundary_unavailable"})
		}
	}
	// A changed file is directly affected even when declaration parsing failed.
	for _, change := range req.Changes {
		if _, exists := req.After[change.Path]; !exists {
			continue
		}
		seed := Seed{ID: change.Path, Path: change.Path, Version: "after", Granularity: "file", Basis: "direct_change"}
		routes[change.Path] = impactPath{Path: codegraph.Path{Nodes: []string{change.Path}, Relations: []codegraph.Relation{}, Confidence: codegraph.Exact}, Version: "after", Seed: seed}
	}
	seen := map[string]bool{}
	gapCandidates := tests
	if len(req.TestDirs) == 0 {
		for name := range req.After {
			if codegraph.Language(name) != "" {
				gapCandidates = append(gapCandidates, name)
			}
		}
		sort.Strings(gapCandidates)
	}
	sort.Slice(gaps, func(i, j int) bool {
		if gaps[i].path != gaps[j].path {
			return gaps[i].path < gaps[j].path
		}
		if gaps[i].message != gaps[j].message {
			return gaps[i].message < gaps[j].message
		}
		return gaps[i].version < gaps[j].version
	})
	for _, issue := range gaps {
		key := issue.path + "\x00" + issue.message + "\x00" + issue.version
		if seen[key] {
			continue
		}
		seen[key] = true
		u := boundGap(graphs, issue, req, gapCandidates)
		// Keep gaps outside the candidates' known dependency paths visible.
		// This is not a claim that unknown runtime relationships are absent.
		if len(u.TestFiles) == 0 && u.Scope != "repository" {
			r.Observations = append(r.Observations, u)
			continue
		}
		r.Uncertainties = append(r.Uncertainties, u)
		text := issue.message
		if issue.path != "" {
			text = issue.path + ": " + text
		}
		r.FallbackReasons = append(r.FallbackReasons, text)
	}
	r.FallbackReasons = unique(r.FallbackReasons)
	if len(r.FallbackReasons) > 0 {
		r.Scope = "partial"
	}
	files := make([]string, 0, len(routes))
	for name := range routes {
		files = append(files, name)
	}
	sort.Strings(files)
	for _, name := range files {
		route := routes[name]
		r.AffectedFiles = append(r.AffectedFiles, FileImpact{Path: name, Version: route.Version, Seed: route.Seed,
			Confidence: route.Confidence, Distance: route.Distance, DependencyPath: route.Nodes, Relations: route.Relations})
	}
	for _, test := range tests {
		if route, ok := routes[test]; ok {
			kind := "import"
			if len(route.Nodes) == 1 {
				kind = "changed_test"
			}
			r.TestFiles = append(r.TestFiles, test)
			r.Reasons = append(r.Reasons, Reason{TestFile: test, Kind: kind, Version: route.Version, DependencyPath: route.Nodes,
				Relations: route.Relations, Seed: route.Seed, Confidence: route.Confidence, Distance: route.Distance})
		}
	}
	return nil
}

func skippedGaps(req Request) []gap {
	var gaps []gap
	for name, message := range req.Skipped {
		// Unchanged non-source links/assets are not executable dependencies. Imports
		// of them still yield a resolver gap; changed resources are expanded below.
		if isSourceOrConfig(name) {
			gaps = append(gaps, gap{path: name, reason: "skipped_dependency", message: message, component: true})
		}
	}
	for _, message := range req.Issues {
		name, detail, ok := strings.Cut(message, ": ")
		if !ok {
			name = ""
			detail = message
		}
		gaps = append(gaps, gap{path: name, reason: "snapshot_incomplete", message: detail, global: true})
	}
	return gaps
}

func isSourceOrConfig(name string) bool {
	return codegraph.Language(name) != "" || broadChange(name)
}
