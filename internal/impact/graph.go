package impact

import (
	"context"
	"path"
	"sort"
	"strings"

	"github.com/compforge/repocli/internal/syntax"
	"golang.org/x/mod/modfile"
)

type graph struct {
	reverse   map[string]map[string]bool
	potential map[string]map[string]bool // Used only to scope diagnostics, never to select tests.
}

func newGraph() *graph {
	return &graph{reverse: map[string]map[string]bool{}, potential: map[string]map[string]bool{}}
}

func (g *graph) link(importer, dependency string) {
	if importer == dependency {
		return
	}
	if g.reverse[dependency] == nil {
		g.reverse[dependency] = map[string]bool{}
	}
	g.reverse[dependency][importer] = true
}

func (g *graph) index(ctx context.Context, files map[string][]byte, a *syntax.Analyzer, gitlinks map[string]bool) ([]gap, error) {
	var issues []gap
	names := make([]string, 0, len(files))
	modules := map[string]string{}
	for name, data := range files {
		names = append(names, name)
		if path.Base(name) == "go.mod" {
			m, err := modfile.Parse(name, data, nil)
			if err != nil || m.Module == nil {
				issues = append(issues, gap{path: name, message: "cannot resolve Go module", component: true})
				continue
			}
			modules[path.Dir(name)] = m.Module.Mod.Path
			for _, replace := range m.Replace {
				if replace.New.Version == "" {
					issues = append(issues, gap{path: name, message: "local replace directives are not resolved", component: true})
				}
			}
		}
	}
	sort.Strings(names)
	resolver := newResolver(files, modules)
	resolver.gitlinks = gitlinks
	issues = append(issues, resolver.configIssues...)
	// Compiler config inheritance is a dependency too: a shared base config may
	// belong to another component even when no application imports cross over.
	for config, parents := range resolver.configDependencies {
		for _, parent := range parents {
			g.link(config, parent)
		}
	}

	// Directory membership suggests compiler-config applicability, but includes,
	// excludes and build invocation are not resolved. Keep that relation out of
	// the evidenced graph so it cannot manufacture a test association.
	for config := range files {
		if !strings.HasPrefix(path.Base(config), "tsconfig") || !strings.HasSuffix(config, ".json") {
			continue
		}
		if g.potential[config] == nil {
			g.potential[config] = map[string]bool{}
		}
		dir := path.Dir(config)
		for name := range files {
			language := syntax.Language(name)
			if (language == "typescript" || language == "tsx" || language == "javascript") && (dir == "." || strings.HasPrefix(name, dir+"/")) {
				g.potential[config][name] = true
			}
		}
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		language := syntax.Language(name)
		if language == "" {
			continue
		}
		if ignoredDependency(name) {
			continue
		}
		facts := a.Analyze(ctx, name, files[name])
		for public, local := range facts.Exports {
			g.link(symbolKey(name, public), symbolKey(name, local))
		}
		for _, root := range facts.SearchPaths {
			captured := false
			for file := range files {
				if root == "." || strings.HasPrefix(file, root+"/") {
					captured = true
					break
				}
			}
			if !captured {
				issues = append(issues, gap{path: name, message: "Python search path is outside captured contents: " + root})
			}
		}
		for _, issue := range facts.Issues {
			issues = append(issues, gap{path: name, message: issue})
		}
		if language == "go" {
			// Package nodes model Go's implicit same-package dependencies without
			// a quadratic set of file-to-file edges. Test files don't affect users
			// of the production package, but do affect sibling package tests.
			pkg := "package:" + path.Dir(name)
			if IsTest(name) {
				g.link(name, pkg)
				g.link("tests:"+path.Dir(name), name)
				g.link(name, "tests:"+path.Dir(name))
			} else {
				g.link(pkg, name)
				g.link(name, pkg)
			}
		}
		for _, imp := range facts.Imports {
			deps, issue := resolver.resolve(name, language, imp)
			if issue != "" {
				issues = append(issues, gap{path: name, message: issue})
				continue
			}
			for _, dep := range deps {
				g.link(name, dep) // File-wide changes always affect named imports too.
				named := imp.Names
				if language == "python" && strings.TrimSuffix(path.Base(dep), ".py") == path.Base(strings.ReplaceAll(imp.Path, ".", "/")) {
					// `from pkg import module` imports that module, not a same-named
					// declaration inside it.
					named = nil
				}
				if len(named) == 0 {
					g.link(name, "any-symbol:"+dep)
				} else {
					for _, symbol := range named {
						g.link(name, symbolKey(dep, symbol))
					}
				}
			}
		}
	}
	return issues, nil
}

func symbolKey(file, name string) string { return "symbol:" + file + "#" + name }

func ignoredDependency(name string) bool {
	for _, part := range strings.Split(name, "/") {
		switch part {
		case "node_modules", "vendor", ".venv", "venv", ".git":
			return true
		}
	}
	return false
}

// affected uses a sorted multi-source BFS so explanations are stable shortest
// dependency paths, including across cycles and equal-length alternatives.
func (g *graph) affected(seeds []string) map[string][]string { return g.walk(seeds, false) }

func (g *graph) potentiallyAffected(seeds []string) map[string][]string { return g.walk(seeds, true) }

func (g *graph) walk(seeds []string, potential bool) map[string][]string {
	routes := map[string][]string{}
	queue := unique(seeds)
	for _, seed := range queue {
		routes[seed] = []string{seed}
	}
	for i := 0; i < len(queue); i++ {
		current := queue[i]
		var callers []string
		for caller := range g.reverse[current] {
			callers = append(callers, caller)
		}
		if potential {
			for caller := range g.potential[current] {
				callers = append(callers, caller)
			}
		}
		callers = unique(callers)
		for _, caller := range callers {
			if _, seen := routes[caller]; seen {
				continue
			}
			routes[caller] = append([]string{caller}, routes[current]...)
			queue = append(queue, caller)
		}
	}
	return routes
}
