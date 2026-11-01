package project

import (
	"reflect"
	"testing"

	"github.com/compforge/quality-harness/sdks/go/common"
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
		".hidden/other/go.mod":          nil,
	}
	l, err := Load(files, "git@github.com:example/mono.git")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range l.Components {
		got[c.Root] = c.Language
	}
	want := map[string]string{".": "", "docs": "", "server": "python", "server/embedded": "javascript", "cli": "typescript", "legacy": "python"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("components: %+v", l.Components)
	}
	if l.Owner("shared.py").Root != "." {
		t.Fatal("missing Makefile root ownership")
	}
	if l.Owner("server/embedded/a.ts").Root != "server/embedded" {
		t.Fatal("nested manifest did not establish its own component")
	}
	if l.Repository.Path != "example/mono" || l.Repository.Forge.Name != "github" {
		t.Fatal(l.Repository)
	}
}

func TestRootComponentAndUnownedSources(t *testing.T) {
	l, err := Load(map[string][]byte{"go.mod": []byte("module example/root\n"), "backend/pyproject.toml": nil}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Components) != 2 || l.Components[0].Root != "." || l.Components[0].Language != "go" {
		t.Fatal(l.Components)
	}
	l, err = Load(map[string][]byte{"a.ts": []byte("export const a = 1;")}, "")
	if err != nil || len(l.Components) != 0 || l.Repository != nil {
		t.Fatalf("%+v %v", l, err)
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

func TestLegacyConfigurationIsIgnored(t *testing.T) {
	for _, config := range []string{`{`, `{"components":[]}`, `{"repository":{"path":"override"}}`} {
		l, err := Load(map[string][]byte{".repocli.json": []byte(config), "Makefile": nil}, "")
		if err != nil || len(l.Components) != 1 || l.Components[0].Root != "." || l.Components[0].Language != "" || l.Repository != nil {
			t.Fatalf("configuration affected discovery: %+v, %v", l, err)
		}
	}
}

// The repository root is less specific even when a child root is one character.
func TestRootAndSingleCharacterComponentOwnership(t *testing.T) {
	layout, err := Load(map[string][]byte{"go.mod": nil, "a/package.json": []byte(`{}`)}, "")
	if err != nil {
		t.Fatal(err)
	}
	if owner := layout.Owner("a/main.js"); owner == nil || owner.Root != "a" {
		t.Fatalf("wrong nested owner: %+v", owner)
	}
	if owner := layout.Owner("root.go"); owner == nil || owner.Root != "." {
		t.Fatalf("wrong root owner: %+v", owner)
	}
}
