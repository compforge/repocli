package impact

import (
	"context"
	"sort"
	"strings"

	"github.com/compforge/repocli/internal/codegraph"
	"github.com/compforge/repocli/internal/project"
)

type gap struct {
	path, message, reason string
	component             bool
	global                bool
}

// Uncertainty records where dependency analysis is incomplete. Candidate
// reachability is internal bookkeeping only: uncertain tests are never output.
type Uncertainty struct {
	Reason          string               `json:"reason,omitempty"`
	Relation        codegraph.Kind       `json:"relation,omitempty"`
	Confidence      codegraph.Confidence `json:"confidence,omitempty"`
	Version         string               `json:"version,omitempty"`
	Line            int                  `json:"line,omitempty"`
	PossibleTargets []string             `json:"possibleTargets,omitempty"`
	Disposition     string               `json:"disposition,omitempty"`
	Path            string               `json:"path,omitempty"`
	Message         string               `json:"message"`
	Scope           string               `json:"scope"`
	TestFiles       []string             `json:"-"`
}

func boundGap(graphs []*codegraph.Graph, issue gap, req Request, tests []string) Uncertainty {
	u := Uncertainty{Reason: issue.reason, Path: issue.path, Message: issue.message, Scope: "dependency", TestFiles: []string{}}
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

func (r *Result) selectTests(ctx context.Context, graphs []*codegraph.Graph, builds []codegraph.BuildResult, req Request, tests, seeds []string, gaps []gap) error {
	routes := impactPaths(graphs, seeds, false)
	// A test's own diff is direct evidence even when graph expansion was limited.
	// It does not require an invented path in a version where the file is absent.
	for _, change := range req.Changes {
		if _, exists := req.After[change.Path]; exists && IsTest(change.Path) {
			routes[change.Path] = impactPath{Path: codegraph.Path{Nodes: []string{change.Path}}, Version: "after"}
		}
	}
	// Membership proven on either version does not depend on another uncertain
	// route. Query only remaining candidates, independently on each version.
	var remaining []string
	for _, test := range tests {
		if _, ok := routes[test]; !ok {
			remaining = append(remaining, test)
		}
	}
	kinds := impactKinds
	if req.Mode == "file" {
		kinds = []codegraph.Kind{codegraph.Imports, codegraph.Reexports, codegraph.PackageMember, codegraph.ConfigExtends}
	}
	for index, built := range builds {
		query, err := built.Query(ctx, seeds, remaining, kinds)
		if err != nil {
			return err
		}
		version := "before"
		if index == 1 {
			version = "after"
		}
		convert := func(issue codegraph.QueryIssue) Uncertainty {
			return Uncertainty{Path: issue.Path, Message: issue.Message, Scope: "dependency", TestFiles: issue.Candidates,
				Reason: issue.Code, Relation: issue.Kind, Confidence: issue.Confidence, Version: version, Line: issue.Line,
				PossibleTargets: issue.Targets, Disposition: issue.Disposition}
		}
		for _, issue := range query.Blocking {
			r.Uncertainties = append(r.Uncertainties, convert(issue))
			r.FallbackReasons = append(r.FallbackReasons, issue.Path+": "+issue.Message)
		}
		for _, issue := range query.Observations {
			r.Observations = append(r.Observations, convert(issue))
		}
	}
	seen := map[string]bool{}
	sort.Slice(gaps, func(i, j int) bool {
		if gaps[i].path != gaps[j].path {
			return gaps[i].path < gaps[j].path
		}
		return gaps[i].message < gaps[j].message
	})
	for _, issue := range gaps {
		key := issue.path + "\x00" + issue.message
		if seen[key] {
			continue
		}
		seen[key] = true
		u := boundGap(graphs, issue, req, tests)
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
	for _, test := range tests {

		if route, ok := routes[test]; ok {
			kind := "import"
			if len(route.Nodes) == 1 {
				kind = "changed_test"
			}
			r.TestFiles = append(r.TestFiles, test)
			r.Reasons = append(r.Reasons, Reason{TestFile: test, Kind: kind, Version: route.Version, DependencyPath: route.Nodes, Relations: route.Relations})
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
