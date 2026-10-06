package project

import (
	"reflect"
	"testing"
)

func TestManifestMarkersDoNotDetermineComponentIdentity(t *testing.T) {
	files := map[string][]byte{
		"api/go.mod":                 []byte("module example.com/service\n"),
		"api/testdata/corpus/go.mod": []byte("module example.com/fixture\n"),
		"python/pyproject.toml":      []byte("[tool.ruff]\nline-length = 88\n"),
		"web/package.json":           []byte(`{"name":"@example/ui","packageManager":"pnpm@10.0.0"}`),
	}
	layout, err := Load(files, "")
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
	// Explicit ownership can promote a fixture without changing marker files.
	files[".repocli.json"] = []byte(`{"components":[{"name":"corpus","root":"api/testdata/corpus"}]}`)
	layout, err = Load(files, "")
	if err != nil || len(layout.Components) != 1 || layout.Components[0].Name != "corpus" {
		t.Fatalf("layout: %+v, err: %v", layout, err)
	}
}
