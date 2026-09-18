package analysis

import (
	"github.com/compforge/repocli/internal/diff"
	"github.com/compforge/repocli/internal/impact"
	"github.com/compforge/repocli/internal/project"
)

func componentResults(before, after project.Layout, changes []diff.Change, result impact.Result, diagnostics []Diagnostic) []project.ComponentImpact {
	groups := project.Group(before, after, changes, result.SourceFiles, result.TestFiles)
	for i := range groups {
		group := &groups[i]
		group.Scope = "focused"
		group.Complete = true
		if result.Scope == "not_requested" {
			group.Scope = "not_requested"
		}
		for _, u := range result.Uncertainties {
			matches := u.Scope == "repository"
			for _, test := range u.TestFiles {
				if owner := after.Owner(test); owner != nil && owner.Root == group.Root {
					matches = true
				}
			}
			if matches {
				group.Scope = "partial"
				group.Complete = false
				message := u.Message
				if u.Path != "" {
					message = u.Path + ": " + message
				}
				group.FallbackReasons = append(group.FallbackReasons, message)
			}
		}
		// Capture failures cannot be scoped through an incomplete dependency graph.
		for _, d := range diagnostics {
			if d.Code == "impact_uncertain" && result.Scope != "not_requested" {
				continue
			}
			if d.Code == "impact_uncertain" {
				owner := after.Owner(d.Path)
				if group.Snapshot == "before" {
					owner = before.Owner(d.Path)
				}
				if owner != nil && owner.Root != group.Root {
					continue
				}
			}
			group.Complete = false
			if result.Scope != "not_requested" {
				group.Scope = "partial"
			}
			group.FallbackReasons = append(group.FallbackReasons, d.Message)
		}
	}
	return groups
}
