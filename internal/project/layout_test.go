package project

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/compforge/quality-harness/sdks/go/common"
	"github.com/compforge/repocli/internal/diff"
)

func TestDiscoverComponentsAndLanguages(t *testing.T) {
	files := map[string][]byte{
		"Makefile": nil, "requirements.txt": nil, "tsconfig.json": nil,
		"server/pyproject.toml":         nil,
		"server/embedded/package.json":  []byte(`{}`),
		"cli/package.json":              []byte(`{"devDependencies":{"typescript":"*"}}`),
		"legacy/setup.py":               nil,
		"docs/Makefile":                 nil,
		"node_modules/lib/package.json": []byte(`{}`),
		".worktrees/other/go.mod":       nil,
	}
	l, err := Load(files, "git@github.com:example/mono.git")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range l.Components {
		got[c.Root] = c.Language
	}
	want := map[string]string{"server": "python", "cli": "typescript", "legacy": "python"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("components: %+v", l.Components)
	}
	if l.Owner("shared.py") != nil {
		t.Fatal("invented root ownership")
	}
	if l.Owner("server/embedded/a.ts").Root != "server" {
		t.Fatal("nested manifest split its component")
	}
	if l.Repository.Path != "example/mono" || l.Repository.Forge.Name != "github" {
		t.Fatal(l.Repository)
	}
}

func TestRootComponentAndFallback(t *testing.T) {
	l, err := Load(map[string][]byte{"go.mod": []byte("module example/root\n"), "backend/pyproject.toml": nil}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Components) != 2 || l.Components[0].Root != "." || l.Components[0].Language != "go" {
		t.Fatal(l.Components)
	}
	l, err = Load(map[string][]byte{"a.ts": []byte("export const a = 1;")}, "")
	if err != nil || len(l.Components) != 1 || l.Components[0].Language != "typescript" || l.Repository != nil {
		t.Fatalf("%+v %v", l, err)
	}
	if len(l.Components[0].Products) != 0 {
		t.Fatal("inferred a product from source language")
	}
}

func TestExplicitProductsAreManyToManyAndUseCommonIdentities(t *testing.T) {
	config := []byte(`{"repository":{"forge":{"name":"github"},"path":"example/mono"},"components":[{"name":"shared","root":"lib","products":[{"name":"first"},{"name":"second"}]},{"name":"web","root":"web","products":[{"name":"first"}]}]}`)
	l, err := Load(map[string][]byte{".repocli.json": config, "lib/go.mod": nil, "web/package.json": []byte(`{}`)}, "")
	if err != nil {
		t.Fatal(err)
	}
	groups := Group(l, l, []diff.Change{{Path: "lib/a.go", Status: "modified"}}, []string{"lib/a.go"}, []string{"web/a.test.js"})
	if len(groups) != 2 {
		t.Fatal(groups)
	}
	var identity common.Component = groups[0].Component
	if identity.Repository.Path != "example/mono" || len(groups[0].Products) != 2 {
		t.Fatal(groups)
	}
	if len(groups[0].SourceFiles) != 1 || len(groups[1].TestFiles) != 1 {
		t.Fatal(groups)
	}
}

func TestDeletedComponentRetainsBeforeIdentity(t *testing.T) {
	before, _ := Load(map[string][]byte{"server/pyproject.toml": nil}, "https://github.com/example/mono.git")
	after, _ := Load(map[string][]byte{"cli/package.json": []byte(`{}`)}, "https://github.com/example/mono.git")
	groups := Group(before, after, []diff.Change{{Path: "server/a.py", Status: "deleted"}}, []string{"server/a.py"}, nil)
	if len(groups) != 2 {
		t.Fatal(groups)
	}
	for _, g := range groups {
		if g.Root == "server" && (g.Snapshot != "before" || len(g.SourceFiles) != 1) {
			t.Fatal(g)
		}
	}
}

func TestOriginIdentityDoesNotExposeCredentials(t *testing.T) {
	r := FromOrigin("https://user:secret@github.com/example/repo.git?token=hidden")
	if r == nil || *r != (common.Repository{Forge: common.Forge{Name: "github"}, Path: "example/repo"}) {
		t.Fatalf("unexpected repository identity")
	}
	if FromOrigin("/tmp/local-repo") != nil {
		t.Fatal("local path treated as forge identity")
	}
}

func TestInvalidLayoutIsAnError(t *testing.T) {
	for _, config := range []string{`{`, `{"components":[{"name":"a","root":"../outside"}]}`, `{"components":[{"name":"a","root":"lib"},{"name":"a","root":"web"}]}`} {
		if _, err := Load(map[string][]byte{".repocli.json": []byte(config)}, ""); err == nil {
			t.Errorf("accepted %s", config)
		}
	}
}

func TestSharedLanguageMetadataAndDerivedEcosystem(t *testing.T) {
	l, err := Load(map[string][]byte{
		".repocli.json":       []byte(`{"components":[{"name":"api","root":"server","language":"typescript"}]}`),
		"server/package.json": []byte(`{}`),
	}, "https://github.com/example/repo.git")
	if err != nil {
		t.Fatal(err)
	}
	groups := Group(l, l, nil, nil, nil)
	if len(groups) != 1 || groups[0].Component.Language != "typescript" || groups[0].Component.Ecosystem() != "node" {
		t.Fatalf("explicit language lost: %+v", groups)
	}
	data, err := json.Marshal(groups[0])
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if _, duplicate := wire["language"]; duplicate {
		t.Fatal("language duplicated outside common.Component")
	}
	var component map[string]any
	if err := json.Unmarshal(wire["component"], &component); err != nil {
		t.Fatal(err)
	}
	if component["language"] != "typescript" {
		t.Fatal(component)
	}
	if _, duplicate := component["ecosystem"]; duplicate {
		t.Fatal("derived ecosystem was persisted")
	}
	unknown, err := Load(map[string][]byte{"README.md": nil}, "")
	if err != nil || unknown.Components[0].Language != "" || unknown.Components[0].Ecosystem() != "" {
		t.Fatalf("unknown metadata: %+v, %v", unknown, err)
	}
}
