package codegraph

import (
	"cmp"
	"container/heap"
	"context"
	"slices"
	"strings"

	shared "github.com/compforge/codegraph"
)

// Diagnostic preserves local extraction and repository-resolution gaps.
type Diagnostic struct {
	Path, Code, Message string
	Kind                Kind
	Line                int
	Subject             shared.DiagnosticSubject
	Location            shared.Location
	Outline             *shared.OutlineCoverage
}

type QueryResult struct{ Paths map[string]Path }

// EvidenceRank orders evidence for repocli's policy without rewriting native
// CodeGraph confidence. Candidate and weak edges have the same inference tier.
func EvidenceRank(c Confidence) int {
	switch c {
	case "", Exact:
		return 0
	case Strong:
		return 1
	default:
		return 2
	}
}

func dependencyStep(kind Kind) int {
	switch kind {
	case Contains, PackageMember, Reexports:
		return 0
	default:
		return 1
	}
}

// ComparePaths prefers evidence strength, then dependency distance, then a
// stable shortest explanation. Distances are ranking signals, not probabilities.
func ComparePaths(a, b Path) int {
	if c := cmp.Compare(EvidenceRank(a.Confidence), EvidenceRank(b.Confidence)); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Distance, b.Distance); c != 0 {
		return c
	}
	if c := cmp.Compare(len(a.Relations), len(b.Relations)); c != 0 {
		return c
	}
	return strings.Compare(strings.Join(a.Nodes, "\x00"), strings.Join(b.Nodes, "\x00"))
}

type routeStep struct {
	node            string
	distance, steps int
}
type routeQueue []routeStep

func (q routeQueue) Len() int { return len(q) }
func (q routeQueue) Less(i, j int) bool {
	if q[i].distance != q[j].distance {
		return q[i].distance < q[j].distance
	}
	if q[i].steps != q[j].steps {
		return q[i].steps < q[j].steps
	}
	return q[i].node < q[j].node
}
func (q routeQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *routeQueue) Push(x any)   { *q = append(*q, x.(routeStep)) }
func (q *routeQueue) Pop() any     { old := *q; x := old[len(old)-1]; *q = old[:len(old)-1]; return x }

// Query selects the strongest path to each candidate, then the shortest one
// within that tier. Three bounded searches avoid losing a short weaker prefix
// that becomes preferable after a later weak edge. Zero-cost ownership edges
// project callers to files without treating their siblings as called symbols.
// +spec=`Uncertain relations remain evidence; missing paths do not prove independence`
func (g *Graph) Query(ctx context.Context, seeds, candidates []string, kinds []Kind) (QueryResult, error) {
	allowed := map[Kind]bool{}
	for _, kind := range kinds {
		allowed[kind] = true
	}
	seedFiles := map[string]bool{}
	for _, id := range seeds {
		if n, ok := g.Nodes[id]; ok && (n.Kind == "symbol" || n.Kind == "binding") {
			seedFiles[n.File] = true
		}
	}
	result := QueryResult{Paths: map[string]Path{}}
	for rank := 0; rank <= 2; rank++ {
		if err := ctx.Err(); err != nil {
			return QueryResult{}, err
		}
		next := map[string]Relation{}
		distances := map[string]routeStep{}
		queue := &routeQueue{}
		for _, seed := range unique(seeds) {
			if _, ok := g.Nodes[seed]; !ok {
				continue
			}
			step := routeStep{node: seed}
			distances[seed], next[seed] = step, Relation{}
			heap.Push(queue, step)
		}
		for queue.Len() > 0 {
			if err := ctx.Err(); err != nil {
				return QueryResult{}, err
			}
			current := heap.Pop(queue).(routeStep)
			if distances[current.node] != current {
				continue
			}
			edges := g.Incoming(current.node)
			slices.SortFunc(edges, compareRelations)
			for _, edge := range edges {
				if err := ctx.Err(); err != nil {
					return QueryResult{}, err
				}
				if !allowed[edge.Kind] || EvidenceRank(edge.Confidence) > rank {
					continue
				}
				// A symbol seed must not immediately widen to its whole file.
				// Other reached declarations may project to their containing file.
				if edge.Kind == Contains && seedFiles[edge.From] {
					continue
				}
				step := routeStep{node: edge.From, distance: current.distance + dependencyStep(edge.Kind), steps: current.steps + 1}
				if old, ok := distances[edge.From]; ok && (old.distance < step.distance || old.distance == step.distance && old.steps <= step.steps) {
					continue
				}
				distances[edge.From], next[edge.From] = step, edge
				heap.Push(queue, step)
			}
		}
		targets := candidates
		if targets == nil {
			for node := range distances {
				targets = append(targets, node)
			}
			slices.Sort(targets)
		}
		for _, target := range targets {
			if _, exists := result.Paths[target]; exists {
				continue
			}
			step, reached := distances[target]
			if !reached {
				continue
			}
			route := Path{Confidence: Exact, Distance: step.distance}
			for node := target; ; {
				if err := ctx.Err(); err != nil {
					return QueryResult{}, err
				}
				route.Nodes = append(route.Nodes, node)
				edge := next[node]
				if edge.To == "" {
					break
				}
				if EvidenceRank(edge.Confidence) > EvidenceRank(route.Confidence) {
					route.Confidence = Strong
					if EvidenceRank(edge.Confidence) == 2 {
						route.Confidence = Weak
					}
				}
				route.Relations = append(route.Relations, edge)
				node = edge.To
			}
			result.Paths[target] = route
		}
	}
	return result, nil
}

func compareRelations(a, b Relation) int {
	if c := cmp.Compare(a.From, b.From); c != 0 {
		return c
	}
	if c := cmp.Compare(EvidenceRank(a.Confidence), EvidenceRank(b.Confidence)); c != 0 {
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
	if c := cmp.Compare(a.Basis, b.Basis); c != 0 {
		return c
	}
	return cmp.Or(cmp.Compare(a.ID, b.ID), cmp.Compare(a.Confidence, b.Confidence),
		cmp.Compare(a.To, b.To), cmp.Compare(a.Location.Path, b.Location.Path),
		cmp.Compare(a.Location.StartByte, b.Location.StartByte), cmp.Compare(a.Location.EndByte, b.Location.EndByte),
		cmp.Compare(a.Location.Line, b.Location.Line), cmp.Compare(a.Location.Column, b.Location.Column),
		cmp.Compare(a.Location.EndLine, b.Location.EndLine), cmp.Compare(a.Location.EndColumn, b.Location.EndColumn))
}
