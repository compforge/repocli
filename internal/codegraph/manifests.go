package codegraph

import (
	"context"
	"path"
	"slices"

	shared "github.com/compforge/codegraph"
)

// BuildManifests prepares graph facts from captured configuration, without
// parsing source. Selection belongs to repocli; classification and declarations
// belong to codegraph. Fixture manifests remain facts, not implicit Components.
func BuildManifests(ctx context.Context, files map[string][]byte) (*shared.Graph, error) {
	var names []string
	for name := range files {
		if ignoredDependency(name) {
			continue
		}
		switch path.Base(name) {
		case "go.mod", "pyproject.toml", "package.json":
			names = append(names, name)
		}
	}
	slices.Sort(names)
	docs := make([]shared.Document, 0, len(names))
	for _, name := range names {
		docs = append(docs, shared.Document{Path: name, Content: files[name]})
	}
	g, _, err := shared.Build(ctx, "manifests", docs, shared.Options{MaxDocuments: 4000, MaxSourceBytes: 64 << 20})
	return g, err
}

// GoModules reads explicit graph declarations, never reconstructs a module name
// from paths. Repository resolution remains responsible for import traversal.
func GoModules(g *shared.Graph) map[string]string {
	modules := map[string]string{}
	for _, relation := range g.Relations() {
		if relation.Kind != shared.Declares {
			continue
		}
		from, ok := g.Node(relation.Source)
		if !ok || from.Kind != shared.DocumentNodeKind || from.DocumentKind != shared.ManifestDocument || from.Manifest == nil || from.Manifest.Format != "gomod" || from.Location == nil {
			continue
		}
		to, ok := g.Node(relation.Target)
		if ok && to.Kind == shared.Module {
			modules[path.Dir(from.Location.Path)] = to.Name
		}
	}
	return modules
}
