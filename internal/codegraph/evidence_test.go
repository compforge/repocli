package codegraph

import (
	"context"
	"encoding/json"
	shared "github.com/compforge/codegraph"
	"reflect"
	"testing"
)

func TestNativeEvidenceDetachedAndSerialized(t *testing.T) {
	support := shared.Location{Path: "a.ts", Line: 1}
	edge := Relation{ID: "call-site", From: "a", To: "b", Kind: Calls, Confidence: Scoped,
		Evidence: []shared.Evidence{
			{Basis: "method_name", Confidence: shared.NameOnly},
			{Basis: "receiver_type", Confidence: shared.Scoped, Location: &support},
		}}
	g := New()
	g.AddRelation(edge)
	g.AddRelation(edge)
	edge.Evidence[0].Basis = "mutated"
	support.Path = "mutated"
	incoming := g.Incoming("b")
	if len(incoming) != 1 || len(incoming[0].Evidence) != 2 || incoming[0].Evidence[0].Basis != "method_name" || incoming[0].Evidence[1].Location.Path != "a.ts" {
		t.Fatal(incoming)
	}
	incoming[0].Evidence[1].Location.Path = "mutated-return"
	p := g.Reverse([]string{"b"}, []Kind{Calls})["a"]
	if p.Confidence != Strong || p.Relations[0].Evidence[1].Location.Path != "a.ts" {
		t.Fatal(p)
	}
	data, err := json.Marshal(p.Relations[0])
	if err != nil {
		t.Fatal(err)
	}
	var decoded Relation
	if err := json.Unmarshal(data, &decoded); err != nil || !reflect.DeepEqual(decoded, p.Relations[0]) {
		t.Fatal(decoded, err)
	}
}

func TestSharedNamespaceProjectionPreservesDeclarations(t *testing.T) {
	files := map[string][]byte{
		"a.go": []byte("package app;func Target(){}"),
		"b.go": []byte("package app;func Caller(){Target()}"),
	}
	b, err := NewBuilder(BuildOptions{Files: files, Kinds: []Kind{Calls, Contains}, MaxFiles: 10, MaxDepth: 3})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Add(context.Background(), "a.go", "b.go"); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.sources[""]; ok {
		t.Fatal("organization became empty-path source")
	}
	got := b.Result()
	for _, n := range got.Graph.Nodes {
		if n.File == "" {
			t.Fatal(n)
		}
	}
	calls := got.Graph.Outgoing(SymbolID("b.go", "Caller"))
	if len(calls) != 1 || calls[0].To != SymbolID("a.go", "Target") || calls[0].Confidence != Exact || len(calls[0].Evidence) == 0 {
		t.Fatal(calls)
	}
}
