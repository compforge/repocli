package codegraph

import (
	"path"
	"strings"

	shared "github.com/compforge/codegraph"
)

type resolvedImport struct {
	reference  shared.FactImport
	targets    []string
	confidence Confidence
	basis      string
}

// Prefix entries are known to precede the unknown launch-time search path.
// Conditional/append roots are useful evidence, but cannot prove precedence.
type pythonPaths struct{ prefix, possible []string }

func (r *resolver) pythonResolve(name string, imp shared.FactImport, paths pythonPaths) (resolvedImport, *Issue) {
	result := resolvedImport{reference: imp}
	keys := []string{imp.Path}
	if imp.From != "" {
		keys = append(keys, imp.From)
	}
	if imp.Relative > 0 {
		dir := path.Dir(name)
		for i := 1; i < imp.Relative; i++ {
			if dir == "." {
				return result, importIssue("unresolved_import", "relative Python import escapes captured package", nil)
			}
			dir = path.Dir(dir)
		}
		// Empty from-module names refer to the current package's attributes.
		if imp.From == "" && len(imp.Names) > 0 {
			keys = append(keys, "")
		}
		found := r.pythonAtRoot(dir, keys)
		if len(found) == 0 {
			return result, importIssue("unresolved_import", "unresolved relative Python import: "+imp.Path, nil)
		}
		result.targets = r.pythonInitializers(found)
		result.basis = "python_relative"
		if len(found) > 1 {
			result.confidence = Strong
			return result, importIssue("ambiguous_import", "relative Python import has multiple targets: "+imp.Path, result.targets)
		}
		return result, nil
	}
	for _, root := range paths.prefix {
		found := r.pythonAtRoot(root, keys)
		if len(found) > 0 {
			result.targets = r.pythonInitializersWithin(found, root)
			result.basis = "python_search_path"
			if len(found) > 1 {
				result.confidence = Strong
				return result, importIssue("ambiguous_import", "Python search-path import has multiple targets: "+imp.Path, result.targets)
			}
			return result, nil
		}
		// Finding a regular top-level package here also prevents later roots
		// from supplying a missing child. Do not silently skip that shadowing.
		top := strings.Split(imp.Path, ".")[0]
		if len(r.pythonAtRoot(root, []string{top})) > 0 {
			return result, importIssue("unresolved_import", "Python package shadows an unavailable child: "+imp.Path, nil)
		}
	}
	for _, root := range paths.possible {
		result.targets = append(result.targets, r.pythonAtRoot(root, keys)...)
	}
	if len(result.targets) > 0 {
		result.confidence = Strong
		result.basis = "python_possible_search_path"
	} else {
		result.confidence = Weak
		result.basis = "python_catalog_candidate"
	}
	// Without a proven prefix, every captured source-root match is a possible
	// target. A singleton catalog match is still not a resolved import.
	known := map[string]bool{}
	for _, target := range r.pythonInitializers(result.targets) {
		known[target] = true
	}
	catalog := r.pythonTargets(keys)
	for _, target := range catalog {
		if !known[target] {
			result.confidence = Weak
			result.basis = "python_catalog_candidate"
		}
	}
	result.targets = append(result.targets, catalog...)
	result.targets = r.pythonInitializers(unique(result.targets))
	if len(result.targets) == 0 {
		top := strings.Split(imp.Path, ".")[0]
		if len(r.python[top]) > 0 {
			return result, importIssue("unresolved_import", "unresolved local Python import: "+imp.Path, nil)
		}
		return result, nil // No captured local candidate: external in this model.
	}
	// Keep all candidates at the weaker level if some came only from name matching.
	return result, nil
}

func (r *resolver) pythonAtRoot(root string, keys []string) []string {
	var found []string
	for _, key := range keys {
		base := path.Join(root, strings.ReplaceAll(key, ".", "/"))
		for _, candidate := range []string{base + ".py", path.Join(base, "__init__.py")} {
			if _, ok := r.files[candidate]; ok {
				found = append(found, candidate)
			}
		}
	}
	return unique(found)
}

func (r *resolver) pythonInitializers(found []string) []string {
	return r.pythonInitializersWithin(found, ".")
}

func (r *resolver) pythonInitializersWithin(found []string, root string) []string {
	result := append([]string{}, found...)
	for _, file := range found {
		for dir := path.Dir(file); dir != root && dir != "."; dir = path.Dir(dir) {
			init := path.Join(dir, "__init__.py")
			if _, ok := r.files[init]; ok {
				result = append(result, init)
			}
		}
	}
	return unique(result)
}
