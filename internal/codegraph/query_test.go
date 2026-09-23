package codegraph

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
)

func mustQuery(t *testing.T, g *Graph, seeds, candidates []string, kinds []Kind) QueryResult {
	t.Helper()
	result, err := g.Query(context.Background(), seeds, candidates, kinds)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

type cancelAfterChecks struct {
	context.Context
	cancel    context.CancelFunc
	remaining int
}

func (c *cancelAfterChecks) Err() error {
	c.remaining--
	if c.remaining <= 0 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestQueryCancelsDuringTraversal(t *testing.T) {
	g := New()
	for i := 0; i < 100; i++ {
		g.AddRelation(Relation{From: fmt.Sprint(i + 1), To: fmt.Sprint(i), Kind: Imports})
	}
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &cancelAfterChecks{Context: base, cancel: cancel, remaining: 6}
	result, err := g.Query(ctx, []string{"0"}, []string{"100"}, []Kind{Imports})
	if !errors.Is(err, context.Canceled) || result.Paths != nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestQueryKnownTargetsAndConfidence(t *testing.T) {
	g := New()
	g.AddRelation(Relation{From: "bridge", To: "seed", Kind: Imports, Confidence: Weak, Basis: "catalog"})
	g.AddRelation(Relation{From: "test", To: "bridge", Kind: Imports})
	g.AddRelation(Relation{From: "bridge", To: "test", Kind: Imports}) // cycle
	g.AddRelation(Relation{From: "unrelated", To: "missing", Kind: Imports})
	g.AddRelation(Relation{From: "owner", To: "seed", Kind: Contains})
	q := mustQuery(t, g, []string{"seed"}, []string{"test", "unrelated", "owner"}, []Kind{Imports})
	if len(q.Paths) != 1 || !slices.Equal(q.Paths["test"].Nodes, []string{"test", "bridge", "seed"}) {
		t.Fatal(q)
	}
	edge := q.Paths["test"].Relations[1]
	if edge.Confidence != Weak || edge.Basis != "catalog" {
		t.Fatalf("lost inference: %+v", edge)
	}
	// Prefer exact metadata when parallel edges have identical endpoints.
	g.AddRelation(Relation{From: "bridge", To: "seed", Kind: Imports})
	q = mustQuery(t, g, []string{"seed"}, []string{"test"}, []Kind{Imports})
	if q.Paths["test"].Relations[1].Confidence != "" {
		t.Fatal(q)
	}
	if len(mustQuery(t, g, []string{"seed"}, []string{}, []Kind{Imports}).Paths) != 0 {
		t.Fatal("empty candidate set")
	}
}

func TestQueryDoesNotInferOwnership(t *testing.T) {
	g := New()
	seed := SymbolID("a.ts", "a")
	caller := SymbolID("b.ts", "b")
	g.AddRelation(Relation{From: caller, To: seed, Kind: Calls, Confidence: Strong})
	g.AddRelation(Relation{From: "consumer.ts", To: "b.ts", Kind: Imports})
	q := mustQuery(t, g, []string{seed}, []string{caller, "consumer.ts"}, []Kind{Calls, Imports})
	if len(q.Paths) != 1 || q.Paths[caller].Relations[0].Confidence != Strong {
		t.Fatal(q)
	}
}

func TestUnknownCallTargetsAreOmitted(t *testing.T) {
	built, err := Build(context.Background(), BuildRequest{BuildOptions: BuildOptions{
		Files: map[string][]byte{"api.ts": []byte("function target() { return 1; }\nfunction wrapper(target) { return target(); }\n")},
		Kinds: []Kind{Imports, Contains, Calls}, MaxFiles: 10, MaxDepth: 2}, FilesToExpand: []string{"api.ts"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(built.Diagnostics) != 0 || len(built.Graph.Outgoing(SymbolID("api.ts", "wrapper"))) != 0 {
		t.Fatal(built)
	}
	if _, ok := built.Graph.Nodes[SymbolID("api.ts", "target")]; !ok {
		t.Fatal("lost declaration")
	}
}

func TestAmbiguousTargetExpansionLimitIsReported(t *testing.T) {
	built, err := Build(context.Background(), BuildRequest{BuildOptions: BuildOptions{Files: map[string][]byte{
		"client.ts": []byte("import {x} from './a';"), "a.ts": []byte("import {x} from './seed';"),
		"a.js": []byte("export const x=1;"), "seed.ts": []byte("export const x=2;"),
	}, Kinds: []Kind{Imports}, MaxFiles: 10, MaxDepth: 0}, FilesToExpand: []string{"seed.ts", "client.ts"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(built.Diagnostics) == 0 || built.Diagnostics[0].Code != "expansion_limit" {
		t.Fatal(built)
	}
	q := mustQuery(t, built.Graph, []string{"seed.ts"}, []string{"client.ts"}, []Kind{Imports})
	if len(q.Paths) != 0 {
		t.Fatal("invented unexpanded dependency", q)
	}
}

func BenchmarkQueryLargeGraph(b *testing.B) {
	g := New()
	candidates := []string{}
	for i := 0; i < 33559; i++ {
		name := fmt.Sprint(i)
		if i > 0 {
			g.AddRelation(Relation{From: name, To: fmt.Sprint((i - 1) / 2), Kind: Imports, Confidence: Weak})
		}
		if i >= 33519 {
			candidates = append(candidates, name)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := g.Query(context.Background(), []string{"0"}, candidates, []Kind{Imports}); err != nil {
			b.Fatal(err)
		}
	}
}

func TestConfigResourcesAreNotSourceCatalog(t *testing.T) {
	req := BuildRequest{BuildOptions: BuildOptions{Files: map[string][]byte{
		"tsconfig.json": []byte(`{"extends":"./child/tsconfig"}`),
		"client.ts":     []byte("export const value=1;"),
	}, Resources: map[string][]byte{
		"child/tsconfig.json": []byte(`{"compilerOptions":{"strict":true}}`),
		"child/hidden.ts":     []byte("invalid ((("),
	}, Gitlinks: map[string]bool{"child": true}, Kinds: []Kind{Imports, ConfigScope, ConfigExtends}, MaxFiles: 10, MaxDepth: 2}, FilesToExpand: []string{"client.ts"}}
	built, err := Build(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(built.Diagnostics) != 0 || !slices.Equal(built.ParsedFiles, []string{"client.ts"}) {
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
	if len(built.Diagnostics) != 1 || built.Diagnostics[0].Code != "boundary_unavailable" {
		t.Fatalf("%+v", built)
	}
	delete(req.Gitlinks, "child")
	built, err = Build(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(built.Diagnostics) != 1 || built.Diagnostics[0].Code != "missing_config" {
		t.Fatalf("%+v", built)
	}
}

func TestQueryMultipleSeedsAndStablePaths(t *testing.T) {
	edges := []Relation{
		{From: "test", To: "b", Kind: Imports}, {From: "test", To: "a", Kind: Imports},
		{From: "a", To: "seed-a", Kind: Imports, Confidence: Weak},
		{From: "b", To: "seed-b", Kind: Imports},
	}
	a, b := New(), New()
	for i, edge := range edges {
		a.AddRelation(edge)
		b.AddRelation(edges[len(edges)-1-i])
	}
	x := mustQuery(t, a, []string{"seed-b", "seed-a"}, []string{"test"}, []Kind{Imports})
	y := mustQuery(t, b, []string{"seed-a", "seed-b"}, []string{"test"}, []Kind{Imports})
	if !reflect.DeepEqual(x, y) || !slices.Equal(x.Paths["test"].Nodes, []string{"test", "a", "seed-a"}) {
		t.Fatalf("%+v != %+v", x, y)
	}
}
