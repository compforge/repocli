package project

import (
	"reflect"
	"testing"
)

func TestToolEvidenceAndBoundaries(t *testing.T) {
	files := map[string][]byte{
		"package.json":                         []byte(`{"packageManager":"pnpm@10.0.0"}`),
		"pnpm-lock.yaml":                       nil,
		"package-lock.json":                    nil,
		"web/package.json":                     []byte(`{"devDependencies":{"typescript":"*"}}`),
		"server/pyproject.toml":                nil,
		"server/uv.lock":                       nil,
		"server/poetry.lock":                   nil,
		"cli/go.mod":                           nil,
		"legacy/setup.py":                      nil,
		"legacy/requirements.txt":              nil,
		"broken/package.json":                  []byte(`{`),
		"broken/yarn.lock":                     nil,
		"node_modules/dependency/package.json": []byte(`{"packageManager":"bun@1.0.0"}`),
	}
	layout, err := Load(files, "")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]toolObservation{}
	for _, c := range layout.Components {
		got[c.Root] = []toolObservation{}
		if c.Tools != nil {
			for _, tool := range *c.Tools {
				got[c.Root] = append(got[c.Root], toolObservation{Name: tool.Name, Version: tool.Version, Evidence: c.ToolEvidence[tool.Name]})
			}
		}
	}
	want := map[string][]toolObservation{
		".":      {{Name: "npm", Evidence: []string{"package-lock.json"}}, {Name: "pnpm", Version: "10.0.0", Evidence: []string{"package.json#packageManager", "pnpm-lock.yaml"}}},
		"web":    {},
		"server": {{Name: "poetry", Evidence: []string{"server/poetry.lock"}}, {Name: "uv", Evidence: []string{"server/uv.lock"}}},
		"cli":    {{Name: "go", Evidence: []string{"cli/go.mod"}}},
		"legacy": {},
		"broken": {{Name: "yarn", Evidence: []string{"broken/yarn.lock"}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tools: %#v", got)
	}
}
