// Package codegraph builds and queries caller-scoped source graphs.
// It has no dependency on diff, test discovery, Git, or execution policy.
package codegraph

import (
	"cmp"
	"slices"
	"strings"
)

type Kind string

const (
	Contains      Kind = "contains"
	Imports       Kind = "imports"
	Calls         Kind = "calls"
	Reexports     Kind = "reexports"
	PackageMember Kind = "package_member"
	ConfigExtends Kind = "config_extends"
	ConfigScope   Kind = "config_scope" // Directory applicability, not a proven dependency.
)

type Node struct {
	ID                 string
	Kind               string // file, declared symbol, referenced binding, or module
	File               string
	Name               string
	StartLine, EndLine int // Declaration range; zero for unresolved bindings and aggregate nodes.
}

// Confidence is independent of relation kind. Empty means evidenced; strong
// and weak are inference levels, not calibrated runtime probabilities.
type Confidence string

const (
	Strong Confidence = "strong"
	Weak   Confidence = "weak"
)

type Relation struct {
	Basis      string     `json:"basis,omitempty"`
	Confidence Confidence `json:"confidence,omitempty"`
	From       string     `json:"from"`
	To         string     `json:"to"`
	Kind       Kind       `json:"kind"`
	File       string     `json:"file,omitempty"`
	Line       int        `json:"line,omitempty"`
}

type Path struct {
	Nodes     []string
	Relations []Relation
}

// Graph owns facts from one supplied version. Query relation kinds explicitly:
// ownership and configuration applicability are not dependency assertions.
// +spec=`Traversal never infers a relation from node names or file ownership`
type Graph struct {
	Nodes    map[string]Node
	outgoing map[string][]Relation
	incoming map[string][]Relation
	seen     map[Relation]bool
}

func New() *Graph {
	return &Graph{Nodes: map[string]Node{}, outgoing: map[string][]Relation{}, incoming: map[string][]Relation{}, seen: map[Relation]bool{}}
}

func SymbolID(file, name string) string { return "symbol:" + file + "#" + name }
func ModuleID(file string) string       { return "any-symbol:" + file }

func (g *Graph) AddNode(n Node) { g.Nodes[n.ID] = n }
func (g *Graph) AddRelation(r Relation) {
	if r.From == r.To || g.seen[r] {
		return
	}
	g.ensureEndpoint(r.From)
	g.ensureEndpoint(r.To)
	g.seen[r] = true
	g.outgoing[r.From] = append(g.outgoing[r.From], r)
	g.incoming[r.To] = append(g.incoming[r.To], r)
}

func (g *Graph) Incoming(id string) []Relation { return slices.Clone(g.incoming[id]) }
func (g *Graph) Outgoing(id string) []Relation { return slices.Clone(g.outgoing[id]) }

// Reverse returns stable shortest evidence paths from reached nodes to seeds.
// Only empty-confidence edges are evidence; inferred edges remain inspectable.
// Cycles terminate; an empty kind list traverses no relations.
func (g *Graph) Reverse(seeds []string, kinds []Kind) map[string]Path {
	allowed := map[Kind]bool{}
	for _, kind := range kinds {
		allowed[kind] = true
	}
	paths := map[string]Path{}
	queue := []string{}
	for _, seed := range unique(seeds) {
		if _, exists := g.Nodes[seed]; exists {
			queue = append(queue, seed)
			paths[seed] = Path{Nodes: []string{seed}}
		}
	}
	for i := 0; i < len(queue); i++ {
		current := queue[i]
		edges := g.Incoming(current)
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
			return cmp.Compare(a.Line, b.Line)
		})
		for _, edge := range edges {
			if !allowed[edge.Kind] || edge.Confidence != "" {
				continue
			}
			if _, seen := paths[edge.From]; seen {
				continue
			}
			prior := paths[current]
			paths[edge.From] = Path{Nodes: append([]string{edge.From}, prior.Nodes...), Relations: append([]Relation{edge}, prior.Relations...)}
			queue = append(queue, edge.From)
		}
	}
	return paths
}

func unique(values []string) []string {
	out := slices.Clone(values)
	slices.Sort(out)
	return slices.Compact(out)
}

func ignoredDependency(name string) bool {
	for _, part := range strings.Split(name, "/") {
		switch part {
		case "node_modules", "vendor", ".venv", "venv", ".git":
			return true
		}
	}
	return false
}

// An explicit import names a binding even if its declaration is outside the
// expansion boundary. Preserve that reference without claiming a definition.
func (g *Graph) ensureEndpoint(id string) {
	if _, ok := g.Nodes[id]; ok {
		return
	}
	n := Node{ID: id, Kind: "file", File: id}
	if rest, ok := strings.CutPrefix(id, "symbol:"); ok {
		n.File, n.Name, _ = strings.Cut(rest, "#")
		n.Kind = "binding"
	} else {
		for _, prefix := range []string{"any-symbol:", "package:", "tests:"} {
			if rest, ok := strings.CutPrefix(id, prefix); ok {
				n.Kind = "module"
				n.File = rest
				break
			}
		}
	}
	g.AddNode(n)
}
