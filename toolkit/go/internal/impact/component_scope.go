package impact

import "github.com/compforge/repocli/toolkit/go/internal/project"

// scopeComponents restricts graph inputs to the owners of the diff. Ownership
// is evaluated in both layouts so deletions, moves and new boundaries retain
// their before/after evidence. Unowned changes select only unowned files.
//
// +spec=`Only Components containing diff paths supply graph documents; imports do not expand Component scope`
// +link=repo://docs/diff.md#component-impact
func scopeComponents(req Request) Request {
	roots := map[string]bool{}
	ownerRoot := func(layout project.Layout, name string) string {
		if owner := layout.Owner(name); owner != nil {
			return owner.Root
		}
		return ""
	}
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
	filter := func(files map[string][]byte, layout project.Layout) map[string][]byte {
		selected := make(map[string][]byte)
		for name, content := range files {
			if roots[ownerRoot(layout, name)] {
				selected[name] = content
			}
		}
		return selected
	}
	req.Before = filter(req.Before, req.OldLayout)
	req.After = filter(req.After, req.NewLayout)
	req.BeforeResources = filter(req.BeforeResources, req.OldLayout)
	req.AfterResources = filter(req.AfterResources, req.NewLayout)
	return req
}
