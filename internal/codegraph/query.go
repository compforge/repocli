package codegraph

import (
	"cmp"
	"context"
	"slices"
)

// Diagnostic records an actual extraction, configuration or expansion gap.
// Unknown dependency targets are omitted by the best-effort resolver.
type Diagnostic struct {
	Path    string
	Kind    Kind
	Code    string
	Message string
	Line    int
}

type QueryResult struct{ Paths map[string]Path }

// Query recommends reachable candidates through known-target relations, including
// inferred edges. Confidence and basis stay on the returned explanation edges.
// +spec=`Unknown targets create no relation; absence of a path is not proof of independence`
// +why=`Best-effort test selection needs reachability, not uncertainty fixed points`
func (g *Graph) Query(ctx context.Context, seeds, candidates []string, kinds []Kind) (QueryResult, error) {
	if err := ctx.Err(); err != nil {
		return QueryResult{}, err
	}
	allowed := map[Kind]bool{}
	for _, kind := range kinds {
		allowed[kind] = true
	}
	// A discovered node has one successor toward a seed. Store that edge once;
	// copying whole paths at every hop makes long chains quadratic in memory.
	next := map[string]Relation{}
	queue := []string{}
	for _, seed := range unique(seeds) {
		if _, ok := g.Nodes[seed]; ok {
			next[seed] = Relation{}
			queue = append(queue, seed)
		}
	}
	for head := 0; head < len(queue); head++ {
		if err := ctx.Err(); err != nil {
			return QueryResult{}, err
		}
		edges := g.Incoming(queue[head])
		slices.SortFunc(edges, func(a, b Relation) int {
			if c := cmp.Compare(a.From, b.From); c != 0 {
				return c
			}
			if c := cmp.Compare(a.Kind, b.Kind); c != 0 {
				return c
			}
			if c := cmp.Compare(a.File, b.File); c != 0 {
				return c
			}
			if c := cmp.Compare(a.Line, b.Line); c != 0 {
				return c
			}
			if c := cmp.Compare(a.Confidence, b.Confidence); c != 0 {
				return c
			}
			return cmp.Compare(a.Basis, b.Basis)
		})
		for _, edge := range edges {
			if err := ctx.Err(); err != nil {
				return QueryResult{}, err
			}
			if !allowed[edge.Kind] {
				continue
			}
			if _, seen := next[edge.From]; seen {
				continue
			}
			next[edge.From] = edge
			queue = append(queue, edge.From)
		}
	}
	// nil means all reached nodes; an empty non-nil set requests no paths.
	if candidates == nil {
		candidates = queue
	}
	result := QueryResult{Paths: map[string]Path{}}
	for _, candidate := range candidates {
		if _, ok := next[candidate]; !ok {
			continue
		}
		route := Path{}
		for node := candidate; ; {
			if err := ctx.Err(); err != nil {
				return QueryResult{}, err
			}
			route.Nodes = append(route.Nodes, node)
			edge := next[node]
			if edge.To == "" {
				break
			}
			route.Relations = append(route.Relations, edge)
			node = edge.To
		}
		result.Paths[candidate] = route
	}
	if err := ctx.Err(); err != nil {
		return QueryResult{}, err
	}
	return result, nil
}
