package project

import (
	"encoding/json"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/compforge/quality-harness/sdks/go/common"
	"github.com/compforge/repocli/toolkit/go/internal/diff"
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
		c.PackageTools = detectPackageTools(files, c.Root)
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

// Group retains both sides of moves and ownership changes. Matching by the
// longest directory prefix keeps a nested component out of its parent's group.
func Group(before, after Layout, changes []diff.Change, sources, tests []string) []ComponentImpact {
	groups := map[string]*ComponentImpact{}
	ensure := func(l Layout, binding Binding, snapshot string) *ComponentImpact {
		keyData, _ := json.Marshal(struct {
			Repo       *common.Repository
			Name, Root string
		}{l.Repository, binding.Name, binding.Root})
		key := string(keyData)
		g := groups[key]
		if g == nil {
			g = &ComponentImpact{Component: binding.Component, Root: binding.Root, Snapshot: snapshot, Products: binding.Products, PackageTools: binding.PackageTools, ChangedFiles: []string{}, AffectedFiles: []string{}, SourceFiles: []string{}, TestFiles: []string{}}
			groups[key] = g
		}
		return g
	}
	for _, binding := range after.Components {
		ensure(after, binding, "after")
	}
	add := func(l Layout, lookup, file string, kind string, snapshot string) {
		binding := l.Owner(lookup)
		if binding == nil {
			return
		}
		g := ensure(l, *binding, snapshot)
		g.Affected = true
		switch kind {
		case "change":
			g.ChangedFiles = append(g.ChangedFiles, file)
		case "test":
			g.TestFiles = append(g.TestFiles, file)
		case "source":
			g.SourceFiles = append(g.SourceFiles, file)
		}
	}
	isSource := map[string]bool{}
	for _, name := range sources {
		isSource[name] = true
	}
	for _, c := range changes {
		if c.Status != "deleted" {
			add(after, c.Path, c.Path, "change", "after")
			if isSource[c.Path] {
				add(after, c.Path, c.Path, "source", "after")
			}
		}
		if c.Status != "added" {
			old := c.Path
			if c.OldPath != "" {
				old = c.OldPath
			}
			add(before, old, old, "change", "before")
			if isSource[c.Path] {
				add(before, old, c.Path, "source", "before")
			}
		}
	}
	for _, name := range tests {
		add(after, name, name, "test", "after")
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]ComponentImpact, 0, len(keys))
	for _, key := range keys {
		g := groups[key]
		g.ChangedFiles = dedup(g.ChangedFiles)
		g.SourceFiles = dedup(g.SourceFiles)
		g.TestFiles = dedup(g.TestFiles)
		out = append(out, *g)
	}
	return out
}

func dedup(values []string) []string {
	sort.Strings(values)
	out := make([]string, 0, len(values))
	for _, v := range values {
		if len(out) == 0 || out[len(out)-1] != v {
			out = append(out, v)
		}
	}
	return out
}
