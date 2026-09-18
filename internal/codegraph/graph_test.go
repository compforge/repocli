package codegraph

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"testing"
)

func TestReverseEvidenceAndKinds(t *testing.T) {
	g := New()
	for _, edge := range []Relation{
		{From: "a", To: "seed", Kind: Calls, File: "a.py", Line: 4},
		{From: "b", To: "seed", Kind: Imports},
		{From: "consumer", To: "b", Kind: Imports},
		{From: "consumer", To: "a", Kind: Imports},
		{From: "seed", To: "consumer", Kind: Calls},
		{From: "owner", To: "seed", Kind: Contains},
	} {
		g.AddRelation(edge)
		g.AddRelation(edge)
	}
	routes := g.Reverse([]string{"seed"}, []Kind{Calls, Imports})
	if _, ok := routes["owner"]; ok {
		t.Fatal("ownership became impact")
	}
	if got := routes["consumer"]; !reflect.DeepEqual(got.Nodes, []string{"consumer", "a", "seed"}) || len(got.Relations) != 2 || got.Relations[1].Line != 4 {
		t.Fatalf("path: %+v", got)
	}
	if len(g.Reverse([]string{"seed"}, nil)) != 1 {
		t.Fatal("empty relation set traversed edges")
	}
	if len(g.Incoming("seed")) != 3 {
		t.Fatal("duplicate edges")
	}
	if len(g.Nodes) != 5 {
		t.Fatal(g.Nodes)
	}
}

func TestBuildOnlyRequestedImportClosure(t *testing.T) {
	files := map[string][]byte{
		"src/math.py": []byte("def value():\n    return 1\n"),
		"src/api.py":  []byte("from .math import value\n"),
		"client.py":   []byte("from .src.api import value\n"),
	}
	for i := 0; i < 500; i++ {
		files[fmt.Sprintf("unrelated/%d.py", i)] = []byte("broken syntax (((")
	}
	got, err := Build(context.Background(), BuildRequest{BuildOptions: BuildOptions{Files: files, Kinds: []Kind{Contains, Imports}, MaxDepth: 8, MaxFiles: 10}, FilesToExpand: []string{"src/math.py", "client.py"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.ParsedFiles, []string{"client.py", "src/api.py", "src/math.py"}) || len(got.Issues) != 0 {
		t.Fatalf("%+v", got)
	}
	if _, ok := got.Graph.Reverse([]string{SymbolID("src/math.py", "value")}, []Kind{Imports})["client.py"]; !ok {
		t.Fatal("missing indirect import")
	}
	for _, node := range got.Graph.Nodes {
		for _, edge := range got.Graph.Outgoing(node.ID) {
			if _, ok := got.Graph.Nodes[edge.To]; !ok {
				t.Fatalf("dangling edge: %+v", edge)
			}
		}
	}
}

func TestBuildLimitsKeepKnownBoundary(t *testing.T) {
	for _, req := range []BuildRequest{
		{BuildOptions: BuildOptions{MaxDepth: 0, MaxFiles: 10}},
		{BuildOptions: BuildOptions{MaxDepth: 10, MaxFiles: 1}},
	} {
		req.Files = map[string][]byte{"a.py": []byte("from .b import value\n"), "b.py": []byte("def value(): pass\n")}
		req.FilesToExpand = []string{"a.py"}
		req.Kinds = []Kind{Imports}
		got, err := Build(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got.ParsedFiles, []string{"a.py"}) || len(got.Issues) != 1 || got.Issues[0].Path != "b.py" {
			t.Fatalf("%+v", got)
		}
		if _, ok := got.Graph.Reverse([]string{"b.py"}, []Kind{Imports})["a.py"]; !ok {
			t.Fatal("lost known boundary edge")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Build(ctx, BuildRequest{BuildOptions: BuildOptions{MaxFiles: 1}, FilesToExpand: []string{"a.py"}}); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestAmbiguousImportHasNoGraphEdge(t *testing.T) {
	got, err := Build(context.Background(), BuildRequest{BuildOptions: BuildOptions{Files: map[string][]byte{
		"a.ts": []byte("export const a=1"), "a.js": []byte("export const a=2"),
		"client.ts": []byte("import {a} from './a'"),
	}, Kinds: []Kind{Imports}, MaxFiles: 10, MaxDepth: 3}, FilesToExpand: []string{"client.ts"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Issues) != 1 || len(got.Graph.Outgoing("client.ts")) != 0 || len(got.ParsedFiles) != 3 {
		t.Fatalf("%+v", got)
	}
}

func TestRelationScopeDoesNotExpandGoPackage(t *testing.T) {
	got, err := Build(context.Background(), BuildRequest{BuildOptions: BuildOptions{Files: map[string][]byte{
		"a.go": []byte("package demo\nfunc A() {}\n"), "b.go": []byte("package demo\nfunc B() {}\n"),
	}, Kinds: []Kind{Contains}, MaxDepth: 0, MaxFiles: 10}, FilesToExpand: []string{"a.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.ParsedFiles, []string{"a.go"}) {
		t.Fatal(got.ParsedFiles)
	}
	if len(got.Graph.Reverse([]string{"missing.go"}, []Kind{Imports})) != 0 {
		t.Fatal("unknown node became a path")
	}
	if got.Graph.Nodes[SymbolID("a.go", "A")].StartLine != 2 {
		t.Fatal("lost declaration location")
	}
}
