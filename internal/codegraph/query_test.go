package codegraph

import (
	"context"
	"slices"
	"testing"
)

func TestQueryKeepsUnknownRoutesSeparate(t *testing.T) {
	g := New()
	g.AddNode(Node{ID: "seed", Kind: "file", File: "seed"})
	g.AddRelation(Relation{From: "known", To: "seed", Kind: Imports})
	g.AddRelation(Relation{From: "candidate", To: "loader", Kind: Imports})
	issues := []Issue{
		{Path: "loader", Kind: Imports, Code: "ambiguous_import", Targets: []string{"left", "right"}},
		{Path: "left", Kind: Imports, Code: "dynamic_target"},
		{Path: "loader", Kind: Calls, Code: "binding_ambiguous"},
	}
	q := g.Query([]string{"seed"}, []string{"known", "candidate"}, []Kind{Imports}, issues)
	if _, ok := q.Paths["candidate"]; ok {
		t.Fatal("uncertainty became an evidence path")
	}
	if len(q.Blocking) != 2 || len(q.Observations) != 1 || q.Observations[0].Disposition != "relation_not_requested" {
		t.Fatalf("%+v", q)
	}
	for _, issue := range q.Blocking {
		if !slices.Equal(issue.Candidates, []string{"candidate"}) {
			t.Fatalf("%+v", issue)
		}
	}
	if len(g.Outgoing("loader")) != 0 {
		t.Fatal("query inserted speculative edges")
	}
	// Exhaustively bounded targets without routes or further gaps cannot reach
	// the seed. This is stronger evidence than merely not finding an edge.
	q = g.Query([]string{"seed"}, []string{"candidate"}, []Kind{Imports}, issues[:1])
	if len(q.Blocking) != 0 || len(q.Observations) != 1 {
		t.Fatalf("%+v", q)
	}
	g.AddRelation(Relation{From: "right", To: "seed", Kind: Imports})
	q = g.Query([]string{"seed"}, []string{"candidate"}, []Kind{Imports}, issues[:1])
	if len(q.Blocking) != 1 {
		t.Fatalf("lost possible indirect route: %+v", q)
	}
}

func TestQueryConfidenceAndAlreadyProvenMembership(t *testing.T) {
	g := New()
	g.AddRelation(Relation{From: "candidate", To: "seed", Kind: Imports, Confidence: Strong})
	if len(g.Reverse([]string{"seed"}, []Kind{Imports})) != 1 {
		t.Fatal("candidate edge accepted")
	}
	g.AddRelation(Relation{From: "weak", To: "seed", Kind: Imports, Confidence: Weak})
	uncertain := g.Query([]string{"seed"}, []string{"candidate", "weak"}, []Kind{Imports}, nil)
	if len(uncertain.Blocking) != 2 {
		t.Fatalf("lost inferred edges: %+v", uncertain)
	}
	g.AddRelation(Relation{From: "candidate", To: "seed", Kind: Imports})
	q := g.Query([]string{"seed"}, []string{"candidate"}, []Kind{Imports}, []Issue{{Path: "candidate", Kind: Imports}})
	if len(q.Blocking) != 0 || len(q.Observations) != 3 || q.Paths["candidate"].Relations[0].Confidence != "" {
		t.Fatalf("%+v", q)
	}
}

func TestBuildRequestsOnlyNeededFeatures(t *testing.T) {
	source := []byte("function target() { return 1; }\nfunction wrapper(target) { return target(); }\n")
	req := BuildRequest{Files: map[string][]byte{"api.ts": source}, Roots: []string{"api.ts"}, Kinds: []Kind{Imports, Contains, Calls}, SymbolFiles: []string{}, MaxFiles: 10, MaxDepth: 2}
	shallow, err := Build(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(shallow.Issues) != 0 || len(shallow.Graph.Outgoing(SymbolID("api.ts", "wrapper"))) != 0 {
		t.Fatalf("%+v", shallow)
	}
	if _, ok := shallow.Graph.Nodes[SymbolID("api.ts", "target")]; ok {
		t.Fatal("extracted unrequested outline")
	}
	req.SymbolFiles = []string{"api.ts"}
	detailed, err := Build(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(detailed.Issues) != 1 || detailed.Issues[0].Kind != Calls || detailed.Issues[0].Line != 2 {
		t.Fatalf("%+v", detailed)
	}
}

func TestAmbiguousTargetExpansionLimitStaysUnknown(t *testing.T) {
	req := BuildRequest{Files: map[string][]byte{
		"client.ts": []byte("import {x} from './a';"),
		"a.ts":      []byte("import {x} from './seed';"), "a.js": []byte("export const x=1;"),
		"seed.ts": []byte("export const x=2;"),
	}, Roots: []string{"seed.ts"}, Candidates: []string{"client.ts"}, Kinds: []Kind{Imports}, MaxFiles: 10, MaxDepth: 0}
	built, err := Build(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	q := built.Graph.Query([]string{"seed.ts"}, []string{"client.ts"}, []Kind{Imports}, built.Issues)
	if len(q.Blocking) == 0 {
		t.Fatalf("unexpanded targets were declared unrelated: %+v", q)
	}
	if _, ok := q.Paths["client.ts"]; ok {
		t.Fatal("ambiguous path returned")
	}
}

func TestConfigResourcesAreNotSourceCatalog(t *testing.T) {
	req := BuildRequest{Files: map[string][]byte{
		"tsconfig.json": []byte(`{"extends":"./child/tsconfig"}`),
		"client.ts":     []byte("export const value=1;"),
	}, Resources: map[string][]byte{
		"child/tsconfig.json": []byte(`{"compilerOptions":{"strict":true}}`),
		"child/hidden.ts":     []byte("invalid ((("),
	}, Gitlinks: map[string]bool{"child": true}, Candidates: []string{"client.ts"}, Kinds: []Kind{Imports, ConfigScope, ConfigExtends}, MaxFiles: 10, MaxDepth: 2}
	built, err := Build(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(built.Issues) != 0 || !slices.Equal(built.ParsedFiles, []string{"client.ts"}) {
		t.Fatalf("%+v", built)
	}
	if _, ok := built.Graph.Nodes["child/hidden.ts"]; ok {
		t.Fatal("resource became source")
	}
	if len(built.Graph.Outgoing("tsconfig.json")) != 1 {
		t.Fatal("missing captured extends relation")
	}
	delete(req.Resources, "child/tsconfig.json")
	built, err = Build(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(built.Issues) != 1 || built.Issues[0].Code != "boundary_unavailable" {
		t.Fatalf("%+v", built)
	}
	delete(req.Gitlinks, "child")
	built, err = Build(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(built.Issues) != 1 || built.Issues[0].Code != "missing_config" {
		t.Fatalf("%+v", built)
	}
}

func TestInferredSymbolRouteBlocksPossibleFileImporter(t *testing.T) {
	g := New()
	seed := SymbolID("a.ts", "a")
	caller := SymbolID("b.ts", "b")
	g.AddRelation(Relation{From: caller, To: seed, Kind: Calls, Confidence: Strong})
	g.AddRelation(Relation{From: "consumer.ts", To: "b.ts", Kind: Imports})
	q := g.Query([]string{seed}, []string{"consumer.ts"}, []Kind{Calls, Imports}, nil)
	if len(q.Blocking) != 1 {
		t.Fatalf("lost uncertain symbol/file route: %+v", q)
	}
	if _, ok := q.Paths["consumer.ts"]; ok {
		t.Fatal("inferred ownership route became evidence")
	}
}
