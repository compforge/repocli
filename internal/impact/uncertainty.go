package impact

import (
	"sort"
	"strings"

	"github.com/compforge/repocli/internal/project"
	"github.com/compforge/repocli/internal/syntax"
)

type gap struct {
	path, message string
	component     bool
	global        bool
}

// Uncertainty records where dependency analysis is incomplete. Candidate
// reachability is internal bookkeeping only: uncertain tests are never output.
type Uncertainty struct {
	Path      string   `json:"path,omitempty"`
	Message   string   `json:"message"`
	Scope     string   `json:"scope"`
	TestFiles []string `json:"-"`
}

func (g *graph) boundGap(issue gap, req Request, tests []string) Uncertainty {
	u := Uncertainty{Path: issue.path, Message: issue.message, Scope: "dependency", TestFiles: []string{}}
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
	routes := g.potentiallyAffected(seeds)
	for _, test := range tests {
		if _, ok := routes[test]; ok {
			u.TestFiles = append(u.TestFiles, test)
		}
	}
	return u
}

func (r *Result) selectTests(g *graph, req Request, tests, seeds []string, gaps []gap) {
	routes := g.affected(seeds)
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
		u := g.boundGap(issue, req, tests)
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
			if len(route) == 1 {
				kind = "changed_test"
			}
			r.TestFiles = append(r.TestFiles, test)
			r.Reasons = append(r.Reasons, Reason{TestFile: test, Kind: kind, DependencyPath: route})
		}
	}
}

func skippedGaps(req Request) []gap {
	var gaps []gap
	for name, message := range req.Skipped {
		// Unchanged non-source links/assets are not executable dependencies. Imports
		// of them still yield a resolver gap; changed resources are expanded below.
		if isSourceOrConfig(name) {
			gaps = append(gaps, gap{path: name, message: message, component: true})
		}
	}
	for _, message := range req.Issues {
		name, detail, ok := strings.Cut(message, ": ")
		if !ok {
			name = ""
			detail = message
		}
		gaps = append(gaps, gap{path: name, message: detail, global: true})
	}
	return gaps
}

func isSourceOrConfig(name string) bool {
	return syntax.Language(name) != "" || broadChange(name)
}
