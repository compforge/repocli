package project

import (
	"path"
	"sort"
	"strings"

	"github.com/compforge/quality-harness/sdks/go/common"
)

// Native manifests take priority over Makefile-only operation boundaries.
// Snapshot traversal needs no filesystem-depth truncation.
var ecosystems = []struct {
	name      string
	manifests []string
}{
	{"python", []string{"pyproject.toml", "setup.py"}},
	{"go", []string{"go.mod"}},
	{"node", []string{"package.json"}},
	{"rust", []string{"Cargo.toml"}},
}

// +spec=`Manifest and Makefile Component boundaries remain discoverable at every nesting depth`
// +link=component://docs/repository.md
func discover(files map[string][]byte) []Binding {
	candidates := map[string]struct{}{}
	// A packaging marker is a Component candidate, not a parsed project identity.
	// The same policy serves lightweight inspection and fully captured versions.
	for name := range files {
		if skipManifest(name) {
			continue
		}
		if path.Base(name) == "Makefile" {
			candidates[path.Dir(name)] = struct{}{}
		}
		for _, ecosystem := range ecosystems {
			for _, marker := range ecosystem.manifests {
				if path.Base(name) == marker {
					candidates[path.Dir(name)] = struct{}{}
				}
			}
		}
	}
	roots := make([]string, 0, len(candidates))
	for root := range candidates {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	out := []Binding{}
	for _, root := range roots {
		out = append(out, Binding{Component: common.Component{Name: root, Language: detectLanguage(files, root)}, Root: root, Products: []common.Product{}})
	}

	return out
}

func detectLanguage(files map[string][]byte, root string) string {
	for _, eco := range ecosystems {
		markers := eco.manifests
		for _, marker := range markers {
			if data, ok := files[path.Join(root, marker)]; ok {
				if eco.name != "node" {
					return eco.name
				}
				text := strings.ToLower(string(data))
				if strings.Contains(text, "typescript") || strings.Contains(text, "@types/") {
					return "typescript"
				}
				if _, ok := files[path.Join(root, "tsconfig.json")]; ok {
					return "typescript"
				}
				return "javascript"
			}
		}
	}
	return ""
}

func skipManifest(name string) bool {
	parts := strings.Split(name, "/")
	for _, part := range parts[:len(parts)-1] {
		if strings.HasPrefix(part, ".") {
			return true
		}
		switch part {
		case "node_modules", "vendor", "venv", "env", "dist", "build", "target", "__pycache__", "testdata":
			return true
		}
	}
	return false
}
