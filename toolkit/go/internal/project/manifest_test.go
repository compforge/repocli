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
	// Legacy configuration cannot promote fixtures or override native boundaries.
	files[".repocli.json"] = []byte(`{"components":[{"name":"corpus","root":"api/testdata/corpus"}]}`)
	layout, err = Load(files, "")
	if err != nil || len(layout.Components) != 3 || layout.Owner("api/testdata/corpus/a.go").Name != "api" {
		t.Fatalf("layout: %+v, err: %v", layout, err)
	}
}
