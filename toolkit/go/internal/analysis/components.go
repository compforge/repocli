package analysis

import (
	"slices"

	"github.com/compforge/repocli/toolkit/go/internal/diff"
	"github.com/compforge/repocli/toolkit/go/internal/impact"
	"github.com/compforge/repocli/toolkit/go/internal/project"
)

func componentResults(before, after project.Layout, changes []diff.Change, result impact.Result, diagnostics []Diagnostic) []project.ComponentImpact {
	groups := project.Group(before, after, changes, result.SourceFiles, result.TestFiles)
	for i := range groups {
		group := &groups[i]
		// Dependency reachability includes ordinary source files even when a component
		// has no discovered tests. Ownership must use the graph's own snapshot.
		for _, file := range result.AffectedFiles {
			layout := after
			if file.Version == "before" {
				layout = before
			}
			owner := layout.Owner(file.Path)
			if owner != nil && owner.Root == group.Root && owner.Component == group.Component {
				group.Affected = true
				group.AffectedFiles = append(group.AffectedFiles, file.Path)
			}
		}
		slices.Sort(group.AffectedFiles)
		group.AffectedFiles = slices.Compact(group.AffectedFiles)
		group.Scope = "focused"
		group.Complete = true
		for _, u := range result.Uncertainties {
			matches := u.Scope == "repository"
			// A gap in an owned file is still a component gap when no tests exist.
			for _, layout := range []project.Layout{before, after} {
				if owner := layout.Owner(u.Path); u.Path != "" && owner != nil && owner.Root == group.Root && owner.Component == group.Component {
					matches = true
				}
			}
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
			if d.Code == "impact_uncertain" {
				continue
			}
			group.Complete = false
			group.Scope = "partial"
			group.FallbackReasons = append(group.FallbackReasons, d.Message)
		}
	}
	return groups
}
