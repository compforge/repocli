package impact

import "github.com/compforge/repocli/toolkit/go/internal/project"

// componentScope selects production source owners, independently of test locations.
// +spec=`Diff ownership selects Components; imports and test discovery never expand this set`
// +link=repo://docs/diff.md#component-impact
type componentScope map[string]bool

func selectComponents(req Request) componentScope {
	roots := componentScope{}
	for _, change := range req.Changes {
		if change.Status != "added" {
			name := change.Path
			if change.OldPath != "" {
				name = change.OldPath
			}
			roots[ownerRoot(req.OldLayout, name)] = true
		}
		if change.Status != "deleted" {
			roots[ownerRoot(req.NewLayout, change.Path)] = true
		}
	}
	return roots
}

func ownerRoot(layout project.Layout, name string) string {
	if owner := layout.Owner(name); owner != nil {
		return owner.Root
	}
	return ""
}

func (s componentScope) contains(layout project.Layout, name string) bool {
	return s[ownerRoot(layout, name)]
}
