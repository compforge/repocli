package units

import (
	"context"
	cg "github.com/compforge/codegraph"
	"sort"
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
	common := func(ids []string) map[namespaceKey]cg.NamespaceMatch {
		out := map[namespaceKey]cg.NamespaceMatch{}
		for i, id := range ids {
			if i == 0 {
				for k, v := range scopes[id] {
					out[k] = v
				}
				continue
			}
			for k, v := range out {
				if next, ok := scopes[id][k]; ok {
					v.Depth = max(v.Depth, next.Depth)
					v.Paths = append(append([]cg.Path(nil), v.Paths...), next.Paths...)
					out[k] = v
				} else {
					delete(out, k)
				}
			}
		}
		return out
	}
	blocked := 0
	var merges []NamespaceMerge
	for len(groups) > limit {
		if err := ctx.Err(); err != nil {
			return nil, 0, nil, err
		}
		left, right, depth, height := -1, -1, 0, -1
		var best namespaceKey
		var proof cg.NamespaceMatch
		for i := range groups {
			for j := i + 1; j < len(groups); j++ {
				ids := union(groups[i], groups[j])
				matches := common(ids)
				if len(matches) == 0 {
					continue
				}
				if !fits(ids) {
					blocked++
					continue
				}
				keys := make([]namespaceKey, 0, len(matches))
				for k := range matches {
					keys = append(keys, k)
				}
				sort.Slice(keys, func(i, j int) bool {
					if keys[i].snapshot != keys[j].snapshot {
						return keys[i].snapshot < keys[j].snapshot
					}
					return keys[i].id < keys[j].id
				})
				for _, k := range keys {
					m := matches[k]
					if left < 0 || heights[k] > height || heights[k] == height && m.Depth < depth {
						height = heights[k]
						left, right, depth, best, proof = i, j, m.Depth, k, m
					}
				}
			}
		}
		if left < 0 {
			break
		}
		ids := union(groups[left], groups[right])
		merges = append(merges, NamespaceMerge{best.snapshot, best.id, ids, proof.Paths})
		groups[left] = ids
		groups = append(groups[:right], groups[right+1:]...)
	}
	return groups, blocked, merges, nil
}
