package units

import (
	"context"
	cg "github.com/compforge/codegraph"
)

type NamespaceMerge struct {
	Snapshot    string    `json:"snapshot"`
	Namespace   string    `json:"namespace"`
	FragmentIDs []string  `json:"fragment_ids"`
	Proofs      []cg.Path `json:"proofs"`
}

type namespaceKey struct{ snapshot, id string }

func coalesce(ctx context.Context, groups [][]string, fs map[string]Fragment, before, after *cg.Graph, limit int, fits func([]string) bool) ([][]string, int, []NamespaceMerge, error) {
	scopes := map[string]map[namespaceKey]cg.NamespaceMatch{}
	heights := map[namespaceKey]int{}
	for id, f := range fs {
		if err := ctx.Err(); err != nil {
			return nil, 0, nil, err
		}
		g, as, path := after, f.After, f.Path
		if len(as) == 0 && len(f.Before) > 0 || f.Status == "deleted" {
			g, as, path = before, f.Before, f.OldPath
		}
		if g == nil {
			continue
		}
		ids := fragmentNodes(f, g, g == before)
		if len(ids) == 0 {
			ids = []string{cg.DocumentID(path)}
		}
		matches, err := g.CommonNamespaces(ctx, ids, cg.NamespaceOptions{Kinds: []cg.NodeKind{cg.Package, cg.Module, cg.Namespace}, MinConfidence: cg.Exact})
		if err != nil {
			return nil, 0, nil, err
		}
		scopes[id] = map[namespaceKey]cg.NamespaceMatch{}
		for _, m := range matches {
			key := namespaceKey{g.Snapshot(), m.Node.ID}
			scopes[id][key] = m
			if _, ok := heights[key]; !ok {
				ancestors, e := g.NamespaceAncestors(ctx, m.Node.ID, cg.NamespaceOptions{Kinds: []cg.NodeKind{cg.Package, cg.Module, cg.Namespace}, MinConfidence: cg.Exact})
				if e != nil {
					return nil, 0, nil, e
				}
				heights[key] = len(ancestors)
			}
		}
	}
	return mergeNamespaceGroups(ctx, groups, scopes, heights, limit, fits)
}
