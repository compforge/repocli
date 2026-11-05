package project

import (
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/compforge/quality-harness/sdks/go/common"
)

// Load discovers engineering boundaries from native manifests and Makefiles.
// Repository identity comes from origin; product relationships are not inferred.
func Load(files map[string][]byte, origin string) (Layout, error) {
	l := Layout{Repository: FromOrigin(origin), Components: discover(files)}
	for i := range l.Components {
		c := &l.Components[i]
		if l.Repository != nil {
			c.Repository = *l.Repository
		}
		c.Depot = discoverDepot(files, c.Root)
		observed := detectTools(files, c.Root)
		if len(observed) > 0 {
			tools := make([]common.Tool, 0, len(observed))
			c.ToolEvidence = map[string][]string{}
			for _, tool := range observed {
				tools = append(tools, common.Tool{Name: tool.Name, Version: tool.Version})
				c.ToolEvidence[tool.Name] = tool.Evidence
			}
			c.Tools = &tools
		}
		for name := range files {
			if path.Dir(name) == c.Root && ManifestEcosystem(name) != "" {
				c.Manifests = append(c.Manifests, name)
			}
		}
		sort.Strings(c.Manifests)
	}
	return l, nil
}

func FromOrigin(origin string) *common.Repository {
	var host, repo string
	if strings.Contains(origin, "://") {
		u, err := url.Parse(origin)
		if err != nil || u.Scheme == "file" {
			return nil
		}
		host, repo = u.Hostname(), strings.TrimPrefix(u.Path, "/")
	} else {
		left, right, ok := strings.Cut(origin, ":")
		if !ok {
			return nil
		}
		if _, after, ok := strings.Cut(left, "@"); ok {
			left = after
		}
		host, repo = left, right
	}
	repo = strings.TrimSuffix(strings.TrimSuffix(repo, "/"), ".git")
	if host == "" || repo == "" {
		return nil
	}
	host = strings.ToLower(host)
	switch host {
	case "github.com":
		host = "github"
	case "gitlab.com":
		host = "gitlab"
	}
	return &common.Repository{Forge: common.Forge{Name: host}, Path: repo}
}

// Owner returns the most specific discovered component boundary.
// It describes ownership, not dependency isolation.
func (l Layout) Owner(name string) *Binding {
	var found *Binding
	for i := range l.Components {
		c := &l.Components[i]
		if c.Root == "." || name == c.Root || strings.HasPrefix(name, c.Root+"/") {
			if found == nil || found.Root == "." || len(c.Root) > len(found.Root) {
				found = c
			}
		}
	}
	return found
}
