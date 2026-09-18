package codegraph

import (
	"context"
	"slices"
	"testing"
)

func TestBuilderStartsEmptyAndAddsCandidateClosures(t *testing.T) {
	builder, err := NewBuilder(BuildOptions{Files: map[string][]byte{
		"seed.ts":   []byte("export const value=1;"),
		"test.ts":   []byte("import {value} from './seed';"),
		"unused.ts": []byte("invalid ((("),
	}, SymbolFiles: []string{"seed.ts"}, Kinds: []Kind{Imports}, MaxDepth: 4, MaxFiles: 10})
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
