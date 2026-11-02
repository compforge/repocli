package codegraph

import (
	"context"
	shared "github.com/compforge/codegraph"
	"reflect"
	"testing"
)

// +case:id=semantic_projection,expect=`Impact retains native dependency kinds and proofs; kind selection excludes unrelated operations and bindings`
func TestSemanticDependencyProjection(t *testing.T) {
	files := map[string][]byte{
		"app.go":    []byte("package app\nvar count int\ntype Model struct{}\nfunc handler(){}\nfunc register(f func()){}\nfunc entry(m Model){handler(); register(handler); count += 1; _ = count}"),
		"view.ts":   []byte("function outer(){const value = new URL('./page.html', import.meta.url); function inner(){return value;} return inner;}"),
		"page.html": []byte("hello"),
	}
	kinds := []Kind{Calls, Reads, Writes, UsesType, Captures, UsesResource}
	b, err := NewBuilder(context.Background(), BuildOptions{Files: files, Kinds: kinds, MaxFiles: 10, MaxDepth: 1, Admission: shared.AnalysisAdmission()})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Add(context.Background(), "app.go", "view.ts", "page.html"); err != nil {
		t.Fatal(err)
	}
	result := b.Result()
	native := map[string]shared.Relation{}
	for _, e := range b.sourceGraph.Relations() {
		native[e.ID] = e
	}
	counts := map[Kind]int{}
	for _, e := range result.Graph.seen {
		counts[e.Kind]++
		original, ok := native[e.ID]
		if !ok || e.Kind != original.Kind || e.Location != original.Location || string(e.Confidence) != string(original.Confidence) || !reflect.DeepEqual(e.Evidence, original.Evidence) {
			t.Fatalf("projection lost native fact: %+v original=%+v", e, original)
		}
	}
	for _, kind := range kinds {
		if counts[kind] == 0 {
			t.Errorf("missing %s: %v", kind, counts)
		}
	}
	// Re-project the same source graph with only writes selected. Reads at the
	// same compound assignment must not bypass the consumer's query contract.
	b.enabled = map[Kind]bool{Writes: true}
	filtered := b.Result()
	if len(filtered.Graph.seen) == 0 {
		t.Fatal("missing write dependency")
	}
	for _, e := range filtered.Graph.seen {
		if e.Kind != Writes {
			t.Fatalf("unexpected relation: %+v", e)
		}
	}
}

func TestResourceDiagnosticKeepsSemanticKind(t *testing.T) {
	b, err := NewBuilder(context.Background(), BuildOptions{Files: map[string][]byte{
		"view.ts": []byte("export function render(){return new URL('./missing.html', import.meta.url)}"),
	}, Kinds: []Kind{UsesResource}, MaxFiles: 2, MaxDepth: 0})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Add(context.Background(), "view.ts"); err != nil {
		t.Fatal(err)
	}
	for _, d := range b.Result().Diagnostics {
		if d.Code == "resource_not_loaded" {
			if d.Kind != UsesResource {
				t.Fatalf("lost diagnostic relation: %+v", d)
			}
			return
		}
	}
	t.Fatal("missing resource diagnostic")
}
