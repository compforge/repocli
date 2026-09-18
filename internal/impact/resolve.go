package impact

import (
	"encoding/json"
	"path"
	"sort"
	"strings"

	"github.com/compforge/repocli/internal/syntax"
)

type manifest struct {
	Name                 string                     `json:"name"`
	Dependencies         map[string]json.RawMessage `json:"dependencies"`
	DevDependencies      map[string]json.RawMessage `json:"devDependencies"`
	PeerDependencies     map[string]json.RawMessage `json:"peerDependencies"`
	OptionalDependencies map[string]json.RawMessage `json:"optionalDependencies"`
}

type resolver struct {
	files        map[string][]byte
	modules      map[string]string
	packages     map[string]manifest
	python       map[string][]string
	configIssues []string
}

func newResolver(files map[string][]byte, modules map[string]string) *resolver {
	r := &resolver{files: files, modules: modules, packages: map[string]manifest{}, python: map[string][]string{}}
	for name, data := range files {
		if ignoredDependency(name) {
			continue
		}
		if path.Base(name) == "package.json" {
			var m manifest
			if err := json.Unmarshal(data, &m); err != nil {
				r.configIssues = append(r.configIssues, name+": invalid package manifest")
			} else {
				r.packages[path.Dir(name)] = m
			}
		}
		if strings.HasPrefix(path.Base(name), "tsconfig") {
			// Resolution-affecting options need a proper TS resolver. Do not
			// silently interpret aliased imports as external dependencies.
			for _, option := range []string{"\"paths\"", "\"baseUrl\"", "\"rootDirs\"", "\"extends\""} {
				if strings.Contains(string(data), option) {
					r.configIssues = append(r.configIssues, name+": custom/inherited TypeScript resolution is not supported")
					break
				}
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
	return r
}

func (r *resolver) resolve(name, language string, imp syntax.Import) ([]string, string) {
	switch language {
	case "go":
		return r.goImport(imp.Path)
	case "python":
		return r.pythonImport(name, imp)
	default:
		return r.jsImport(name, imp.Path)
	}
}

func (r *resolver) goImport(spec string) ([]string, string) {
	var roots []string
	for root, module := range r.modules {
		if spec == module || strings.HasPrefix(spec, module+"/") {
			roots = append(roots, root)
		}
	}
	if len(roots) == 0 {
		if strings.HasPrefix(spec, ".") {
			return nil, "relative Go import is not resolved: " + spec
		}
		return nil, "" // imports outside repository modules are external
	}
	sort.Slice(roots, func(i, j int) bool { return len(r.modules[roots[i]]) > len(r.modules[roots[j]]) })
	root := roots[0]
	suffix := strings.TrimPrefix(strings.TrimPrefix(spec, r.modules[root]), "/")
	dir := path.Join(root, suffix)
	for name := range r.files {
		if path.Dir(name) == dir && syntax.Language(name) == "go" && !IsTest(name) {
			return []string{"package:" + dir}, ""
		}
	}
	return nil, "unresolved local Go import: " + spec
}

func (r *resolver) jsImport(name, spec string) ([]string, string) {
	if strings.HasPrefix(spec, ".") {
		base := path.Join(path.Dir(name), spec)
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
		if len(found) > 0 {
			return unique(found), ""
		}
		return nil, "unresolved relative import: " + spec
	}
	pkg := strings.Split(spec, "/")[0]
	if strings.HasPrefix(spec, "@") {
		parts := strings.Split(spec, "/")
		if len(parts) > 1 {
			pkg = strings.Join(parts[:2], "/")
		}
	}
	for _, m := range r.packages {
		if m.Name != "" && pkg == m.Name {
			return nil, "workspace package import requires package exports resolution: " + spec
		}
	}
	if spec == "bun" || spec == "bun:test" || spec == "bun:sqlite" || spec == "bun:ffi" || spec == "bun:jsc" || strings.HasPrefix(spec, "node:") || nodeBuiltin(spec) {
		return nil, ""
	}
	for dir := path.Dir(name); ; dir = path.Dir(dir) {
		if m, ok := r.packages[dir]; ok {
			for _, deps := range []map[string]json.RawMessage{m.Dependencies, m.DevDependencies, m.PeerDependencies, m.OptionalDependencies} {
				if _, ok := deps[pkg]; ok {
					return nil, ""
				}
			}
		}
		if dir == "." {
			break
		}
	}
	return nil, "unresolved package or alias import: " + spec
}

func nodeBuiltin(spec string) bool {
	root := strings.Split(spec, "/")[0]
	for _, name := range strings.Fields("assert async_hooks buffer child_process cluster console constants crypto dgram diagnostics_channel dns domain events fs http http2 https inspector module net os path perf_hooks process punycode querystring readline repl stream string_decoder sys test timers tls trace_events tty url util v8 vm wasi worker_threads zlib") {
		if root == name {
			return true
		}
	}
	return false
}

func (r *resolver) pythonImport(name string, imp syntax.Import) ([]string, string) {
	var found []string
	keys := []string{imp.Path}
	if imp.From != "" {
		keys = append(keys, imp.From)
	}
	if imp.Relative > 0 {
		dir := path.Dir(name)
		for i := 1; i < imp.Relative; i++ {
			dir = path.Dir(dir)
		}
		for _, key := range keys {
			base := path.Join(dir, strings.ReplaceAll(key, ".", "/"))
			for _, candidate := range []string{base + ".py", path.Join(base, "__init__.py")} {
				if _, ok := r.files[candidate]; ok {
					found = append(found, candidate)
				}
			}
		}
		// `from . import constant` can refer to a package attribute.
		if imp.From == "" {
			if _, ok := r.files[path.Join(dir, "__init__.py")]; ok {
				found = append(found, path.Join(dir, "__init__.py"))
			}
		}
		if len(found) == 0 {
			return nil, "unresolved relative Python import: " + imp.Path
		}
	} else {
		for _, key := range keys {
			found = append(found, r.python[key]...)
		}
		if len(found) == 0 {
			// A name with no repository module is external under the documented
			// static source-root model. Runtime path changes are diagnosed separately.
			root := strings.Split(imp.Path, ".")[0]
			if len(r.python[root]) > 0 {
				return nil, "unresolved local Python import: " + imp.Path
			}
			return nil, ""
		}
	}
	for _, file := range append([]string(nil), found...) {
		for dir := path.Dir(file); dir != "."; dir = path.Dir(dir) {
			init := path.Join(dir, "__init__.py")
			if _, ok := r.files[init]; ok {
				found = append(found, init)
			}
		}
	}
	return unique(found), ""
}
