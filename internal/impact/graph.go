package impact

import (
	"slices"
	"strings"

	"github.com/compforge/repocli/internal/codegraph"
)

type impactPath struct {
	codegraph.Path
	Version string
}

var impactKinds = []codegraph.Kind{codegraph.Imports, codegraph.Calls, codegraph.Reexports, codegraph.PackageMember, codegraph.ConfigExtends}

// Each version is traversed independently. Unioning edges first could invent a
// path whose first half existed only before and second half only after the diff.
func impactPaths(graphs []*codegraph.Graph, seeds []string, diagnostics bool) map[string]impactPath {
	kinds := slices.Clone(impactKinds)
	if diagnostics {
		kinds = append(kinds, codegraph.ConfigScope)
	}
	out := map[string]impactPath{}
	for index, g := range graphs {
		version := "before"
		if index == 1 {
			version = "after"
		}
		mergePaths(out, g.Reverse(seeds, kinds), version)
	}
	return out
}

func ignoredDependency(name string) bool {
	for _, part := range strings.Split(name, "/") {
		switch part {
		case "node_modules", "vendor", ".venv", "venv", ".git":
			return true
		}
	}
	return false
}

func mergePaths(out map[string]impactPath, paths map[string]codegraph.Path, version string) {
	for id, route := range paths {
		old, ok := out[id]
		if !ok || len(route.Nodes) < len(old.Nodes) || (len(route.Nodes) == len(old.Nodes) && strings.Join(route.Nodes, "\x00") < strings.Join(old.Nodes, "\x00")) {
			out[id] = impactPath{Path: route, Version: version}
		}
	}
}
