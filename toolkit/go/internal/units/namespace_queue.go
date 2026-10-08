package units

import (
	"container/heap"
	"context"

	cg "github.com/compforge/codegraph"
)

type namespaceGroup struct {
	ids        []string
	depths     map[namespaceKey]int
	generation int
}

type namespaceCandidate struct {
	left, right                     int
	leftGeneration, rightGeneration int
	key                             namespaceKey
	depth, height                   int
}

type namespaceCandidates []namespaceCandidate

func (q namespaceCandidates) Len() int { return len(q) }
func (q namespaceCandidates) Less(i, j int) bool {
	a, b := q[i], q[j]
	if a.height != b.height {
		return a.height > b.height
	}
	if a.depth != b.depth {
		return a.depth < b.depth
	}
	// Stable slots preserve the exhaustive scan's pair order after removals.
	if a.left != b.left {
		return a.left < b.left
	}
	return a.right < b.right
}
func (q namespaceCandidates) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *namespaceCandidates) Push(v any)   { *q = append(*q, v.(namespaceCandidate)) }
func (q *namespaceCandidates) Pop() any {
	old := *q
	n := len(old)
	v := old[n-1]
	*q = old[:n-1]
	return v
}

func namespaceKeyLess(a, b namespaceKey) bool {
	if a.snapshot != b.snapshot {
		return a.snapshot < b.snapshot
	}
	return a.id < b.id
}

func intersectNamespaceDepths(a, b map[namespaceKey]int) map[namespaceKey]int {
	out := map[namespaceKey]int{}
	for k, d := range a {
		if other, ok := b[k]; ok {
			out[k] = max(d, other)
		}
	}
	return out
}

// Only a newly merged group changes candidate eligibility. Keep all other
// candidates, including budget rejections, instead of rechecking them each round.
func mergeNamespaceGroups(ctx context.Context, groups [][]string, scopes map[string]map[namespaceKey]cg.NamespaceMatch, heights map[namespaceKey]int, limit int, fits func([]string) bool) ([][]string, int, []NamespaceMerge, error) {
	states := make([]namespaceGroup, len(groups))
	for i, ids := range groups {
		depths := map[namespaceKey]int{}
		for j, id := range ids {
			if j == 0 {
				for k, m := range scopes[id] {
					depths[k] = m.Depth
				}
				continue
			}
			for k, d := range depths {
				if m, ok := scopes[id][k]; ok {
					depths[k] = max(d, m.Depth)
				} else {
					delete(depths, k)
				}
			}
		}
		states[i] = namespaceGroup{ids: ids, depths: depths}
	}
	blocked := 0
	queue := namespaceCandidates{}
	candidate := func(left, right int) (namespaceCandidate, bool) {
		a, b := states[left], states[right]
		c := namespaceCandidate{left: left, right: right, leftGeneration: a.generation, rightGeneration: b.generation}
		found := false
		for k, d := range a.depths {
			other, ok := b.depths[k]
			if !ok {
				continue
			}
			depth, height := max(d, other), heights[k]
			if !found || height > c.height || height == c.height && (depth < c.depth || depth == c.depth && namespaceKeyLess(k, c.key)) {
				c.key, c.depth, c.height = k, depth, height
				found = true
			}
		}
		if !found {
			return c, false
		}
		if !fits(union(a.ids, b.ids)) {
			blocked++
			return c, false
		}
		return c, true
	}
	for i := range states {
		if err := ctx.Err(); err != nil {
			return nil, 0, nil, err
		}
		for j := i + 1; j < len(states); j++ {
			if c, ok := candidate(i, j); ok {
				queue = append(queue, c)
			}
		}
	}
	heap.Init(&queue)
	remaining := len(groups)
	var merges []NamespaceMerge
	for remaining > limit && queue.Len() > 0 {
		if err := ctx.Err(); err != nil {
			return nil, 0, nil, err
		}
		c := heap.Pop(&queue).(namespaceCandidate)
		a, b := &states[c.left], &states[c.right]
		if a.ids == nil || b.ids == nil || a.generation != c.leftGeneration || b.generation != c.rightGeneration {
			continue
		}
		ids := union(a.ids, b.ids)
		// Proof paths are materialized only for the chosen merge, in fragment order.
		var proofs []cg.Path
		for _, id := range ids {
			proofs = append(proofs, scopes[id][c.key].Paths...)
		}
		merges = append(merges, NamespaceMerge{c.key.snapshot, c.key.id, ids, proofs})
		a.ids = ids
		a.depths = intersectNamespaceDepths(a.depths, b.depths)
		a.generation++
		b.ids = nil
		b.depths = nil
		remaining--
		if remaining <= limit {
			break
		}
		for i := range states {
			if err := ctx.Err(); err != nil {
				return nil, 0, nil, err
			}
			if i == c.left || states[i].ids == nil {
				continue
			}
			if next, ok := candidate(min(i, c.left), max(i, c.left)); ok {
				heap.Push(&queue, next)
			}
		}
	}
	out := make([][]string, 0, remaining)
	for _, s := range states {
		if s.ids != nil {
			out = append(out, s.ids)
		}
	}
	return out, blocked, merges, nil
}
