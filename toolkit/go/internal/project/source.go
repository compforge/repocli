package project

import (
	"path"
	"slices"
	"strings"

	shared "github.com/compforge/codegraph"
)

// IsSourceMaterial recognizes code and manifests without review admission policy.
func IsSourceMaterial(name string) bool {
	tags := materialTags.DocumentTags(shared.Document{Path: name})
	return ManifestEcosystem(name) != "" || slices.Contains(tags, shared.SourceTag) || slices.Contains(tags, shared.ManifestTag)
}

// SourceRoles combines path conventions and graph facts. Framework/naming roles
// are hints, not proof of framework binding; Go runtime entrypoints use only
// CodeGraph Entrypoint facts. Consumers own review and execution policy.
func SourceRoles(name string, g *shared.Graph) []string {
	var roles []string
	tags := materialTags.DocumentTags(shared.Document{Path: name})
	switch {
	case ManifestEcosystem(name) != "" || slices.Contains(tags, shared.ManifestTag):
		roles = append(roles, "manifest")
	case slices.Contains(tags, shared.LockfileTag):
		roles = append(roles, "lock")
	case slices.Contains(tags, shared.SourceTag):
		roles = append(roles, "source")
	}
	base := path.Base(name)
	if (base == "tsconfig.json" || strings.HasPrefix(base, "tsconfig.")) && strings.HasSuffix(base, ".json") {
		roles = append(roles, "manifest")
	}
	if !slices.Contains(roles, "source") {
		return roles
	}
	if IsTestFile(name) || base == "conftest.py" {
		roles = append(roles, "test")
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".py":
		if base == "__main__.py" {
			roles = append(roles, "entrypoint")
		}
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		if strings.TrimSuffix(base, path.Ext(base)) == "main" {
			roles = append(roles, "entrypoint")
		}
	}

	if g == nil {
		return roles
	}
	for _, n := range g.Nodes() {
		if n.Location == nil || n.Location.Path != name {
			continue
		}
		if n.Entrypoint && !slices.Contains(roles, "entrypoint") {
			roles = append(roles, "entrypoint")
		}
		if n.Language != "python" {
			continue
		}
		if n.ReferenceKind == shared.DecoratorReference && len(g.RelationsFrom(n.ID, shared.Decorates)) > 0 {
			name := n.Name
			if n.Receiver != "" {
				name = n.Receiver + "." + name
			}
			parts := strings.Split(strings.ToLower(name), ".")
			if len(parts) < 2 {
				continue
			}
			switch parts[len(parts)-1] {
			case "get", "post", "put", "patch", "delete", "options", "head", "trace", "api_route", "websocket", "websocket_route":
				if !slices.Contains(roles, "handler") {
					roles = append(roles, "handler")
				}
			}
		}
		if base == "main.py" && n.ReferenceKind == shared.CallReference && n.Name == "FastAPI" && !slices.Contains(roles, "entrypoint") {
			roles = append(roles, "entrypoint")
		}
	}
	return roles
}

// IsTestFile supplies the shared repository naming convention used by role and
// impact consumers. conftest.py supplies test support roles but is not a runnable
// test candidate. Naming does not prove execution or production ownership.
func IsTestFile(name string) bool {
	return slices.Contains(materialTags.DocumentTags(shared.Document{Path: name}), shared.TestTag)
}
