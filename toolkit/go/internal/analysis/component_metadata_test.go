package analysis

import (
	"encoding/json"
	"github.com/compforge/quality-harness/sdks/go/common"
	"github.com/compforge/repokit/toolkit/go/internal/diff"
	"github.com/compforge/repokit/toolkit/go/internal/impact"
	"github.com/compforge/repokit/toolkit/go/internal/project"
	"reflect"
	"testing"
)

func groupFixture(before, after project.Layout, changes []diff.Change, sources, tests []string) []project.ComponentImpact {
	groups, _ := componentResults(before, after, &comparison{changes: changes}, impact.Result{SourceFiles: sources, TestFiles: tests}, nil)
	return groups
}

func TestDiscoveredComponentsUseCommonIdentities(t *testing.T) {
	l, err := project.Load(map[string][]byte{"lib/go.mod": nil, "web/package.json": []byte(`{}`)}, "https://github.com/example/mono.git")
	if err != nil {
		t.Fatal(err)
	}
	groups := groupFixture(l, l, []diff.Change{{Path: "lib/a.go", Status: "modified"}}, []string{"lib/a.go"}, []string{"web/a.test.js"})
	if len(groups) != 2 {
		t.Fatal(groups)
	}
	var identity common.Component = groups[0].Component
	if identity.Repository.Path != "example/mono" || len(groups[0].Products) != 0 {
		t.Fatal(groups)
	}
	if len(groups[0].SourceFiles) != 1 || len(groups[1].TestFiles) != 1 {
		t.Fatal(groups)
	}
}

func TestDeletedComponentRetainsBeforeIdentity(t *testing.T) {
	before, _ := project.Load(map[string][]byte{"server/pyproject.toml": nil}, "https://github.com/example/mono.git")
	after, _ := project.Load(map[string][]byte{"cli/package.json": []byte(`{}`)}, "https://github.com/example/mono.git")
	groups := groupFixture(before, after, []diff.Change{{Path: "server/a.py", Status: "deleted"}}, []string{"server/a.py"}, nil)
	if len(groups) != 2 {
		t.Fatal(groups)
	}
	for _, g := range groups {
		if g.Root == "server" && (g.Snapshot != "before" || len(g.SourceFiles) != 1) {
			t.Fatal(g)
		}
	}
}

func TestSharedLanguageMetadataAndDerivedEcosystem(t *testing.T) {
	l, err := project.Load(map[string][]byte{
		"server/package.json": []byte(`{"devDependencies":{"typescript":"*"}}`),
	}, "https://github.com/example/repo.git")
	if err != nil {
		t.Fatal(err)
	}
	groups := groupFixture(l, l, nil, nil, nil)
	if len(groups) != 1 || groups[0].Component.Language != "typescript" || groups[0].Component.Ecosystem() != "node" {
		t.Fatalf("manifest language lost: %+v", groups)
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
	unknown, err := project.Load(map[string][]byte{"README.md": nil}, "")
	if err != nil || len(unknown.Components) != 0 {
		t.Fatalf("unknown metadata: %+v, %v", unknown, err)
	}
}

func TestComponentImpactPreservesToolEvidence(t *testing.T) {
	layout, err := project.Load(map[string][]byte{
		"web/package.json":   []byte(`{"packageManager":"pnpm@10.0.0"}`),
		"web/pnpm-lock.yaml": nil,
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	groups := groupFixture(layout, layout, nil, nil, nil)
	if len(groups) != 1 || !reflect.DeepEqual(groups[0].ToolEvidence, layout.Components[0].ToolEvidence) {
		t.Fatal(groups)
	}
}
