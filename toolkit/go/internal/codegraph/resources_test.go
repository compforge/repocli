package codegraph

import (
	"context"
	"slices"
	"testing"
)

// +spec=Resource roots participate in bounded incremental publication without reparsing source or mutating an earlier graph.
func TestResourceRootCompletesReference(t *testing.T) {
	ctx := context.Background()
	b, err := NewBuilder(ctx, BuildOptions{Files: map[string][]byte{
		"view.ts":   []byte(`export function render() { return new URL('./page.html', import.meta.url); }`),
		"page.html": []byte("hello"),
	}, Kinds: []Kind{References, Contains}, MaxFiles: 2, MaxDepth: 0})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Add(ctx, "view.ts"); err != nil {
		t.Fatal(err)
	}
	old := b.Result()
	if !slices.ContainsFunc(old.Diagnostics, func(d Diagnostic) bool { return d.Code == "resource_not_loaded" }) {
		t.Fatal(old.Diagnostics)
	}
	if err := b.Add(ctx, "page.html"); err != nil {
		t.Fatal(err)
	}
	current := b.Result()
	if len(current.Graph.Incoming("page.html")) != 1 || len(old.Graph.Incoming("page.html")) != 0 {
		t.Fatal("invalid publication")
	}
	if !slices.Equal(current.ParsedFiles, []string{"view.ts"}) {
		t.Fatal(current.ParsedFiles)
	}
	if slices.ContainsFunc(current.Diagnostics, func(d Diagnostic) bool { return d.Code == "resource_not_loaded" || d.Code == "unsupported_language" }) {
		t.Fatal(current.Diagnostics)
	}
}

func TestResourceRootsConsumeDocumentBudget(t *testing.T) {
	b, err := Build(context.Background(), BuildRequest{BuildOptions: BuildOptions{
		Files:    map[string][]byte{"a.html": []byte("a"), "b.html": []byte("b")},
		MaxFiles: 1, MaxDepth: 0,
	}, FilesToExpand: []string{"a.html", "b.html"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(b.Diagnostics, func(d Diagnostic) bool { return d.Path == "b.html" && d.Code == "file_limit" }) {
		t.Fatal(b.Diagnostics)
	}
}
