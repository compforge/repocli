package project

import (
	"path"
	"sort"
	"strings"

	"github.com/compforge/quality-harness/sdks/go/common"
	"github.com/compforge/repocli/internal/syntax"
)

// Component markers and their priority follow devloop's ecosystem discovery.
// Requirements and compiler/build configuration are language clues, not project
// boundaries. Snapshot traversal needs no filesystem-depth truncation.
var ecosystems = []struct {
	name      string
	manifests []string
}{
	{"python", []string{"pyproject.toml", "setup.py"}},
	{"go", []string{"go.mod"}},
	{"node", []string{"package.json"}},
}

func discover(files map[string][]byte) []Binding {
	candidates := map[string]bool{}
	for name := range files {
		if skipManifest(name) {
			continue
		}
		for _, eco := range ecosystems {
			for _, marker := range eco.manifests {
				if path.Base(name) == marker {
					candidates[path.Dir(name)] = true
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
		nested := false
		for _, c := range out {
			if c.Root != "." && strings.HasPrefix(root, c.Root+"/") {
				nested = true
				break
			}
		}
		if !nested {
			out = append(out, Binding{Component: common.Component{Name: root, Language: detectLanguage(files, root)}, Root: root, Products: []common.Product{}})
		}
	}
	// Unlike an execution default, a root component is not invented beside
	// discovered server/cli components. Shared root files may have no owner.
	if len(out) == 0 {
		out = append(out, Binding{Component: common.Component{Name: ".", Language: detectLanguage(files, ".")}, Root: ".", Products: []common.Product{}})
	}
	return out
}

func detectLanguage(files map[string][]byte, root string) string {
	for _, eco := range ecosystems {
		markers := eco.manifests
		if eco.name == "python" {
			markers = append(append([]string{}, markers...), "requirements.txt")
		}
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
	languages := map[string]bool{}
	for name := range files {
		if skipManifest(name) || root != "." && !strings.HasPrefix(name, root+"/") {
			continue
		}
		if language := syntax.Language(name); language != "" {
			if language == "tsx" {
				language = "typescript"
			}
			languages[language] = true
		}
	}
	if len(languages) == 1 {
		for language := range languages {
			return language
		}
	}
	if len(languages) > 1 {
		return "mixed"
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
