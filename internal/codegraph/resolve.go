package codegraph

import (
	"encoding/json"
	"path"
	"sort"
	"strings"

	shared "github.com/compforge/codegraph"
)

type manifest struct {
	Name                 string                     `json:"name"`
	Exports              json.RawMessage            `json:"exports"`
	Main                 string                     `json:"main"`
	Module               string                     `json:"module"`
	Dependencies         map[string]json.RawMessage `json:"dependencies"`
	DevDependencies      map[string]json.RawMessage `json:"devDependencies"`
	PeerDependencies     map[string]json.RawMessage `json:"peerDependencies"`
	OptionalDependencies map[string]json.RawMessage `json:"optionalDependencies"`
}

type resolver struct {
	gitlinks           map[string]bool
	resources          map[string][]byte
	files              map[string][]byte
	modules            map[string]string
	packages           map[string]manifest
	python             map[string][]string
	configIssues       []Diagnostic
	configDependencies map[string][]string
}

func newResolver(files map[string][]byte, modules map[string]string, resources map[string][]byte, gitlinks map[string]bool) *resolver {
	r := &resolver{files: files, resources: resources, gitlinks: gitlinks, modules: modules, packages: map[string]manifest{}, python: map[string][]string{}, configDependencies: map[string][]string{}}
	for name, data := range files {
		if ignoredDependency(name) {
			continue
		}
		if path.Base(name) == "package.json" {
			var m manifest
			if err := json.Unmarshal(data, &m); err != nil {
				r.configIssues = append(r.configIssues, Diagnostic{Path: name, Kind: Imports, Code: "invalid_config", Message: "invalid package manifest"})
			} else {
				r.packages[path.Dir(name)] = m
			}
		}
		if strings.HasSuffix(name, ".py") {
			parts := strings.Split(strings.TrimSuffix(name, ".py"), "/")
			if parts[len(parts)-1] == "__init__" {
				parts = parts[:len(parts)-1]
			}
			// Index possible source roots without importing project code. Multiple
			// matches are all retained, rather than guessing a PYTHONPATH order.
			for i := range parts {
				key := strings.Join(parts[i:], ".")
				r.python[key] = append(r.python[key], name)
			}
		}
	}
	r.checkTSConfigs()
	return r
}

func (r *resolver) resolve(name, language string, imp shared.FactImport) ([]string, *importGuess) {
	switch language {
	case "go":
		return r.goImport(imp.Path)
	default:
		return r.jsImport(name, imp.Path)
	}
}

func (r *resolver) goImport(spec string) ([]string, *importGuess) {
	var roots []string
	for root, module := range r.modules {
		if spec == module || strings.HasPrefix(spec, module+"/") {
			roots = append(roots, root)
		}
	}
	if len(roots) == 0 {
		return nil, nil // imports outside repository modules are external
	}
	sort.Slice(roots, func(i, j int) bool { return len(r.modules[roots[i]]) > len(r.modules[roots[j]]) })
	root := roots[0]
	suffix := strings.TrimPrefix(strings.TrimPrefix(spec, r.modules[root]), "/")
	dir := path.Join(root, suffix)
	for name := range r.files {
		if path.Dir(name) == dir && Language(name) == "go" && !strings.HasSuffix(name, "_test.go") {
			return []string{"package:" + dir}, nil
		}
	}
	return nil, nil
}

func (r *resolver) jsImport(name, spec string) ([]string, *importGuess) {
	if strings.HasPrefix(spec, ".") {
		base := path.Join(path.Dir(name), spec)
		// Parent imports into a submodule depend on the gitlink as an external
		// package boundary. Never parse its sources or discover its tests here.
		for root := range r.gitlinks {
			if base == root || strings.HasPrefix(base, root+"/") {
				return []string{root}, nil
			}
		}
		var candidates []string
		if strings.HasSuffix(base, ".js") || strings.HasSuffix(base, ".jsx") || strings.HasSuffix(base, ".mjs") || strings.HasSuffix(base, ".cjs") {
			baseWithoutExt := strings.TrimSuffix(base, path.Ext(base))
			candidates = append(candidates, baseWithoutExt+".ts", baseWithoutExt+".tsx", baseWithoutExt+".mts", baseWithoutExt+".cts")
		}
		candidates = append(candidates, base)
		for _, ext := range []string{".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs", ".json"} {
			candidates = append(candidates, base+ext, path.Join(base, "index"+ext))
		}
		var found []string
		for _, candidate := range candidates {
			if _, ok := r.files[candidate]; ok {
				found = append(found, candidate)
			}
		}
		found = unique(found)
		if len(found) > 1 {
			return nil, importCandidates("ambiguous_import", found)
		}
		if len(found) > 0 {
			return found, nil
		}
		return nil, nil
	}
	pkg := strings.Split(spec, "/")[0]
	if strings.HasPrefix(spec, "@") {
		parts := strings.Split(spec, "/")
		if len(parts) > 1 {
			pkg = strings.Join(parts[:2], "/")
		}
	}
	var localRoots []string
	for root, m := range r.packages {
		if m.Name != "" && pkg == m.Name {
			localRoots = append(localRoots, root)
		}
	}
	if len(localRoots) == 1 {
		return r.workspaceImport(localRoots[0], spec, pkg)
	}
	if len(localRoots) > 1 {
		var targets []string
		for _, root := range unique(localRoots) {
			found, guess := r.workspaceImport(root, spec, pkg)
			targets = append(targets, found...)
			if guess != nil {
				targets = append(targets, guess.Targets...)
			}
		}
		if len(targets) > 0 {
			return nil, importCandidates("workspace_package_candidates", targets)
		}
		return nil, nil
	}
	return nil, nil // No captured target: omit the relation.
}

// importGuess is transient resolver output; candidates become inferred edges.
type importGuess struct {
	Targets []string
	Basis   string
}

func importCandidates(code string, targets []string) *importGuess {
	return &importGuess{Targets: unique(targets), Basis: code}
}

func (r *resolver) pythonTargets(keys []string) []string {
	var targets []string
	for _, key := range keys {
		targets = append(targets, r.python[key]...)
	}
	// Package initializers can themselves import changed sources.
	for _, name := range append([]string{}, targets...) {
		for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
			init := path.Join(dir, "__init__.py")
			if _, ok := r.files[init]; ok {
				targets = append(targets, init)
			}
		}
	}
	return unique(targets)
}
