// Package codegraph builds and queries caller-scoped source graphs.
// It has no dependency on diff, test discovery, Git, or execution policy.
package codegraph

import (
	"context"
	"slices"
	"strings"

	shared "github.com/compforge/codegraph"
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

// Confidence retains source evidence tiers and repository inference levels.
// Empty means evidenced; strong/weak are consumer policy, not probabilities.
type Confidence string

const (
	Exact     Confidence = "exact"
	Scoped    Confidence = Confidence(shared.Scoped)
	NameOnly  Confidence = Confidence(shared.NameOnly)
	Heuristic Confidence = Confidence(shared.Heuristic)
	Strong    Confidence = "strong"
	Weak      Confidence = "weak"
)

type Relation struct {
	ID         string            `json:"id,omitempty"`
	Location   shared.Location   `json:"location,omitzero"`
	Basis      string            `json:"basis,omitempty"`    // Repository-owned inference rule.
	Evidence   []shared.Evidence `json:"evidence,omitempty"` // Native source proofs, without flattening.
	Confidence Confidence        `json:"confidence,omitempty"`
	From       string            `json:"from"`
	To         string            `json:"to"`
	Kind       Kind              `json:"kind"`
	File       string            `json:"file,omitempty"`
	Line       int               `json:"line,omitempty"`
}

type Path struct {
	Nodes      []string
	Relations  []Relation
	Confidence Confidence
	Distance   int
}

// Graph owns facts from one supplied version. Query relation kinds explicitly:
// ownership and configuration applicability are not dependency assertions.
// +spec=`Traversal never infers a relation from node names or file ownership`
type Graph struct {
	Nodes    map[string]Node
	outgoing map[string][]Relation
	incoming map[string][]Relation
	seen     map[relationKey]Relation
}

func New() *Graph {
	return &Graph{Nodes: map[string]Node{}, outgoing: map[string][]Relation{}, incoming: map[string][]Relation{}, seen: map[relationKey]Relation{}}
}

func SymbolID(file, name string) string { return "symbol:" + file + "#" + name }
func ModuleID(file string) string       { return "any-symbol:" + file }

func (g *Graph) AddNode(n Node) { g.Nodes[n.ID] = n }
func (g *Graph) AddRelation(r Relation) {
	key := relationKey{r.ID, r.From, r.To, r.Kind, r.File, r.Line, r.Location, r.Basis, r.Confidence}
	if _, exists := g.seen[key]; r.From == r.To || exists {
		return
	}
	g.ensureEndpoint(r.From)
	g.ensureEndpoint(r.To)
	r = cloneRelation(r)
	g.seen[key] = r
	g.outgoing[r.From] = append(g.outgoing[r.From], r)
	g.incoming[r.To] = append(g.incoming[r.To], r)
}

func (g *Graph) Incoming(id string) []Relation { return cloneRelations(g.incoming[id]) }
func (g *Graph) Outgoing(id string) []Relation { return cloneRelations(g.outgoing[id]) }

// Reverse returns stable confidence-ranked paths, retaining native edge evidence.
func (g *Graph) Reverse(seeds []string, kinds []Kind) map[string]Path {
	result, _ := g.Query(context.Background(), seeds, nil, kinds)
	return result.Paths
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

// Proof arrays are payload, not a second occurrence identity. Shared relations
// arrive already aggregated by CodeGraph; repository edges retain their rule key.
type relationKey struct {
	ID, From, To string
	Kind         Kind
	File         string
	Line         int
	Location     shared.Location
	Basis        string
	Confidence   Confidence
}

func cloneRelation(r Relation) Relation {
	r.Evidence = slices.Clone(r.Evidence)
	for i := range r.Evidence {
		if r.Evidence[i].Location != nil {
			loc := *r.Evidence[i].Location
			r.Evidence[i].Location = &loc
		}
	}
	return r
}
func cloneRelations(relations []Relation) []Relation {
	out := slices.Clone(relations)
	for i := range out {
		out[i] = cloneRelation(out[i])
	}
	return out
}
