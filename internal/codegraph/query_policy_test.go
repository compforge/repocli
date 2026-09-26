package codegraph

import (
	"slices"
	"testing"
)

// +spec=`Impact paths prefer confidence before dependency distance`
func TestQueryStrongestPathThenDistance(t *testing.T) {
	g := New()
	g.AddRelation(Relation{From: "join", To: "seed", Kind: Calls, Confidence: Candidate, Basis: "name_candidate"})
	g.AddRelation(Relation{From: "bridge", To: "seed", Kind: Calls, Confidence: Exact})
	g.AddRelation(Relation{From: "join", To: "bridge", Kind: Calls, Confidence: Exact})
	g.AddRelation(Relation{From: "outer", To: "join", Kind: Imports, Confidence: Weak})
	q := mustQuery(t, g, []string{"seed"}, []string{"join", "outer"}, []Kind{Calls, Imports})
	if p := q.Paths["join"]; p.Confidence != Exact || p.Distance != 2 {
		t.Fatalf("stronger path lost: %+v", p)
	}
	// After a weak edge both alternatives have the same confidence: the
	// previously weaker prefix now supplies the shorter valid explanation.
	p := q.Paths["outer"]
	if p.Confidence != Weak || p.Distance != 2 || !slices.Equal(p.Nodes, []string{"outer", "join", "seed"}) || p.Relations[1].Confidence != Candidate || p.Relations[1].Basis != "name_candidate" {
		t.Fatalf("shorter weak prefix or native evidence lost: %+v", p)
	}
}

func TestQueryOwnershipProjectsCallersWithoutSiblings(t *testing.T) {
	g := New()
	seed, caller, sibling := SymbolID("a.ts", "a"), SymbolID("b.ts", "b"), SymbolID("b.ts", "other")
	for _, edge := range []Relation{
		{From: "a.ts", To: seed, Kind: Contains}, {From: caller, To: seed, Kind: Calls, Confidence: Strong},
		{From: "b.ts", To: caller, Kind: Contains}, {From: "b.ts", To: sibling, Kind: Contains},
		{From: "consumer.ts", To: "b.ts", Kind: Imports}, {From: "unrelated.ts", To: sibling, Kind: Imports},
		{From: caller, To: "b.ts", Kind: Contains}, // even a zero-cost cycle must terminate
	} {
		g.AddRelation(edge)
	}
	q := mustQuery(t, g, []string{seed}, []string{"a.ts", "b.ts", "consumer.ts", "unrelated.ts"}, []Kind{Contains, Calls, Imports})
	if len(q.Paths) != 2 || q.Paths["b.ts"].Distance != 1 || q.Paths["consumer.ts"].Distance != 2 || q.Paths["consumer.ts"].Confidence != Strong {
		t.Fatalf("%+v", q)
	}
}
