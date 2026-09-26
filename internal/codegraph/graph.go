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

// Confidence is independent of relation kind. Empty means evidenced; strong
// and weak are inference levels, not calibrated runtime probabilities.
type Confidence string

const (
	Exact     Confidence = "exact"
	Candidate Confidence = "candidate"
	Strong    Confidence = "strong"
	Weak      Confidence = "weak"
)

type Relation struct {
	ID         string          `json:"id,omitempty"`
	Location   shared.Location `json:"location,omitzero"`
	Basis      string          `json:"basis,omitempty"`
	Confidence Confidence      `json:"confidence,omitempty"`
	From       string          `json:"from"`
	To         string          `json:"to"`
	Kind       Kind            `json:"kind"`
	File       string          `json:"file,omitempty"`
	Line       int             `json:"line,omitempty"`
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
