package codegraph

import (
	"context"
	"fmt"
	"slices"
	"testing"
)

func TestBuilderStartsEmptyAndAddsCandidateClosures(t *testing.T) {
	builder, err := NewBuilder(BuildOptions{Files: map[string][]byte{
		"seed.ts":   []byte("export const value=1;"),
		"test.ts":   []byte("import {value} from './seed';"),
		"unused.ts": []byte("invalid ((("),
	}, Kinds: []Kind{Imports}, MaxDepth: 4, MaxFiles: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result := builder.Result(); len(result.Graph.Nodes) != 0 || len(result.ParsedFiles) != 0 {
		t.Fatal("construction parsed source")
	}
	if err := builder.Add(context.Background(), "test.ts"); err != nil {
		t.Fatal(err)
	}
	if err := builder.Add(context.Background(), "test.ts"); err != nil {
		t.Fatal(err)
	}
	if result := builder.Result(); !slices.Equal(result.ParsedFiles, []string{"seed.ts", "test.ts"}) || len(result.Issues) != 0 {
		t.Fatalf("%+v", result)
	}
	query := builder.Result().Query([]string{"seed.ts"}, []string{"test.ts"}, []Kind{Imports})
	if _, ok := query.Paths["test.ts"]; !ok {
		t.Fatal("missing candidate path")
	}
}

func TestBuilderBatchUsesConsumerFileBudgetAndOneSourceGraph(t *testing.T) {
	files := map[string][]byte{}
	var roots []string
	for i := 0; i < 260; i++ {
		name := fmt.Sprintf("f%03d.ts", i)
		files[name] = []byte("export function value() { return 1; }")
		roots = append(roots, name)
	}
	builder, err := NewBuilder(BuildOptions{Files: files, MaxFiles: 300, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	graph := builder.sourceGraph
	if err := builder.Add(context.Background(), append(roots, roots[0])...); err != nil {
		t.Fatal(err)
	}
	if graph != builder.sourceGraph || len(graph.Report().Files) != 260 || len(builder.workset) != 260 {
		t.Fatal("workset did not share a graph or respect the consumer budget")
	}
	if result := builder.Result(); len(result.Sources) != 260 || len(result.Sources[roots[0]].Symbols) != 1 {
		t.Fatal("missing batched declarations")
	}
}

func TestBuilderBatchRegistersRootsBeforeDependencyExpansion(t *testing.T) {
	builder, err := NewBuilder(BuildOptions{Files: map[string][]byte{
		"a.ts": []byte("import './bridge';"), "bridge.ts": []byte("import './z';"),
		"z.ts": []byte("export const value = 1;"),
	}, Kinds: []Kind{Imports}, MaxDepth: 0, MaxFiles: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.Add(context.Background(), "z.ts", "a.ts", "z.ts"); err != nil {
		t.Fatal(err)
	}
	result := builder.Result()
	if !slices.Equal(result.ParsedFiles, []string{"a.ts", "z.ts"}) || len(result.Issues) != 1 || result.Issues[0].Path != "bridge.ts" {
		t.Fatalf("unexpected bounded workset: %+v", result)
	}
}

func TestBuilderBatchPreservesLocalCallFileIdentity(t *testing.T) {
	built, err := Build(context.Background(), BuildRequest{BuildOptions: BuildOptions{
		Files: map[string][]byte{
			"a.go": []byte("package p\nfunc Target() {}\n"),
			"b.go": []byte("package p\nfunc Caller() { Target() }\n"),
		}, Kinds: []Kind{Contains, Calls}, MaxDepth: 1, MaxFiles: 10,
	}, FilesToExpand: []string{"a.go", "b.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := built.Graph.Nodes[SymbolID("b.go", "Target")]; exists {
		t.Fatal("cross-file shared call created a false local declaration")
	}
	if len(built.Graph.Outgoing(SymbolID("b.go", "Caller"))) != 0 {
		t.Fatal("cross-file call escaped the consumer's local-call contract")
	}
}

func TestBuilderShallowerAdditionCompletesFrontier(t *testing.T) {
	builder, err := NewBuilder(BuildOptions{Files: map[string][]byte{
		"test.ts":   []byte("import {value} from './bridge';"),
		"bridge.ts": []byte("import {value} from './seed';"),
		"seed.ts":   []byte("export const value=1;"),
	}, Kinds: []Kind{Imports}, MaxDepth: 0, MaxFiles: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"test.ts", "bridge.ts", "seed.ts"} {
		if err := builder.Add(context.Background(), name); err != nil {
			t.Fatal(err)
		}
	}
	if result := builder.Result(); len(result.ParsedFiles) != 3 || len(result.Issues) != 0 {
		t.Fatalf("%+v", result)
	}
	if _, ok := builder.Result().Query([]string{"seed.ts"}, []string{"test.ts"}, []Kind{Imports}).Paths["test.ts"]; !ok {
		t.Fatal("missing completed path")
	}
}

func TestBuilderShallowerVisitExpandsAlreadyParsedDependencies(t *testing.T) {
	builder, err := NewBuilder(BuildOptions{Files: map[string][]byte{
		"test.ts":   []byte("import './bridge';"),
		"bridge.ts": []byte("import './middle';"),
		"middle.ts": []byte("import './seed';"),
		"seed.ts":   []byte("export const value=1;"),
	}, Kinds: []Kind{Imports}, MaxDepth: 2, MaxFiles: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"test.ts", "bridge.ts"} {
		if err := builder.Add(context.Background(), file); err != nil {
			t.Fatal(err)
		}
	}
	result := builder.Result()
	if len(result.ParsedFiles) != 4 || len(result.Issues) != 0 {
		t.Fatalf("%+v", result)
	}
}
