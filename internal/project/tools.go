package project

import (
	"encoding/json"
	"path"
	"slices"
	"strings"
)

// PackageTool records static tool evidence, not an installed tool or a command
// recommendation. Multiple entries retain competing or coexisting tool clues.
type PackageTool struct {
	Name     string   `json:"name"`
	Version  string   `json:"version,omitempty"`
	Evidence []string `json:"evidence"`
}

// detectPackageTools only examines a component's own root. Workspace inheritance
// requires explicit workspace semantics; an ancestor lockfile alone is not proof.
func detectPackageTools(files map[string][]byte, root string) []PackageTool {
	found := map[string]*PackageTool{}
	add := func(name, version, evidence string) {
		tool := found[name]
		if tool == nil {
			tool = &PackageTool{Name: name}
			found[name] = tool
		}
		if version != "" {
			tool.Version = version
		}
		tool.Evidence = append(tool.Evidence, evidence)
	}
	manifest := path.Join(root, "package.json")
	if data, ok := files[manifest]; ok {
		var metadata struct {
			PackageManager string `json:"packageManager"`
		}
		// Discovery remains best effort for malformed package manifests, as for
		// language detection. Lockfiles can still supply independent evidence.
		if json.Unmarshal(data, &metadata) == nil {
			name, version, _ := strings.Cut(metadata.PackageManager, "@")
			switch name {
			case "npm", "pnpm", "yarn", "bun":
				add(name, version, manifest+"#packageManager")
			}
		}
	}
	for _, marker := range []struct{ file, name string }{
		{"go.mod", "go"},
		{"uv.lock", "uv"}, {"poetry.lock", "poetry"}, {"Pipfile.lock", "pipenv"},
		{"package-lock.json", "npm"}, {"npm-shrinkwrap.json", "npm"},
		{"pnpm-lock.yaml", "pnpm"}, {"yarn.lock", "yarn"},
		{"bun.lock", "bun"}, {"bun.lockb", "bun"},
	} {
		name := path.Join(root, marker.file)
		if _, ok := files[name]; ok {
			add(marker.name, "", name)
		}
	}
	names := make([]string, 0, len(found))
	for name := range found {
		names = append(names, name)
	}
	slices.Sort(names)
	tools := make([]PackageTool, 0, len(names))
	for _, name := range names {
		tool := found[name]
		slices.Sort(tool.Evidence)
		tools = append(tools, *tool)
	}
	return tools
}
