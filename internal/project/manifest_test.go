package project

import (
	"context"
	"github.com/compforge/repocli/internal/codegraph"
	"reflect"
	"testing"
)

func loadLayout(files map[string][]byte, origin string) (Layout, error) {
	manifests, err := codegraph.BuildManifests(context.Background(), files)
	if err != nil {
		return Layout{}, err
	}
	return Load(files, origin, manifests)
}

func TestManifestFactsDoNotDetermineComponentIdentity(t *testing.T) {
	files := map[string][]byte{
		"api/go.mod":                 []byte("module example.com/service\n"),
		"api/testdata/corpus/go.mod": []byte("module example.com/fixture\n"),
		"python/pyproject.toml":      []byte("[tool.ruff]\nline-length = 88\n"),
		"web/package.json":           []byte(`{"name":"@example/ui","packageManager":"pnpm@10.0.0"}`),
	}
	graph, err := codegraph.BuildManifests(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := graph.Document("api/testdata/corpus/go.mod"); !ok {
		t.Fatal("fixture manifest must remain a code fact")
	}
	layout, err := Load(files, "", graph)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, component := range layout.Components {
		names = append(names, component.Name)
	}
	if !reflect.DeepEqual(names, []string{"api", "python", "web"}) {
		t.Fatalf("components: %+v", layout.Components)
	}
	if owner := layout.Owner("api/testdata/corpus/a.go"); owner == nil || owner.Name != "api" {
		t.Fatalf("owner: %+v", owner)
	}
	if tools := layout.Owner("web/app.ts").PackageTools; len(tools) != 1 || tools[0].Name != "pnpm" {
		t.Fatalf("tools: %+v", tools)
	}
	// Explicit ownership can promote a fixture without changing graph facts.
	files[".repocli.json"] = []byte(`{"components":[{"name":"corpus","root":"api/testdata/corpus"}]}`)
	layout, err = Load(files, "", graph)
	if err != nil || len(layout.Components) != 1 || layout.Components[0].Name != "corpus" {
		t.Fatalf("layout: %+v, err: %v", layout, err)
	}
}
