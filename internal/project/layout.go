package project

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/compforge/quality-harness/sdks/go/common"
	"github.com/compforge/repocli/internal/diff"
)

// Load uses versioned ownership when declared, otherwise discovers manifest
// boundaries. Products are never inferred from directory or package names.
func Load(files map[string][]byte, origin string) (Layout, error) {
	l := Layout{Repository: FromOrigin(origin), Components: []Binding{}}
	if data, ok := files[".repocli.json"]; ok {
		if err := json.Unmarshal(data, &l); err != nil {
			return l, fmt.Errorf("read .repocli.json: %w", err)
		}
		if l.Repository != nil && (l.Repository.Forge.Name == "" || l.Repository.Path == "") {
			return l, fmt.Errorf(".repocli.json repository requires forge.name and path")
		}
		if len(l.Components) == 0 {
			l.Components = discover(files)
		}
	} else {
		l.Components = discover(files)
	}
	seenRoots, seenNames := map[string]bool{}, map[string]bool{}
	for i := range l.Components {
		c := &l.Components[i]
		if c.Root == "" {
			c.Root = "."
		}
		if c.Name == "" || seenNames[c.Name] || seenRoots[c.Root] || c.Root != "." && !diff.ValidPath(c.Root) {
			return l, fmt.Errorf("invalid or duplicate component name/root: %q (%q)", c.Name, c.Root)
		}
		seenNames[c.Name] = true
		seenRoots[c.Root] = true
		if c.Products == nil {
			c.Products = []common.Product{}
		}
		if l.Repository != nil {
			c.Repository = *l.Repository
		}
		if c.Language == "" {
			c.Language = detectLanguage(files, c.Root)
		}
		productNames := map[string]bool{}
		for _, p := range c.Products {
			if p.Name == "" || productNames[p.Name] {
				return l, fmt.Errorf("component %s has empty or duplicate product name", c.Name)
			}
			productNames[p.Name] = true
		}
		sort.Slice(c.Products, func(i, j int) bool { return c.Products[i].Name < c.Products[j].Name })
	}
	sort.Slice(l.Components, func(i, j int) bool { return l.Components[i].Root < l.Components[j].Root })
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

// Owner returns the most specific declared or discovered component boundary.
// It describes ownership, not dependency isolation.
func (l Layout) Owner(name string) *Binding {
	var found *Binding
	for i := range l.Components {
		c := &l.Components[i]
		if c.Root == "." || name == c.Root || strings.HasPrefix(name, c.Root+"/") {
			if found == nil || len(c.Root) > len(found.Root) {
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
			g = &ComponentImpact{Component: binding.Component, Root: binding.Root, Snapshot: snapshot, Products: binding.Products, SourceFiles: []string{}, TestFiles: []string{}}
			groups[key] = g
		}
		return g
	}
	for _, binding := range after.Components {
		ensure(after, binding, "after")
	}
	add := func(l Layout, lookup, file string, test bool, snapshot string) {
		binding := l.Owner(lookup)
		if binding == nil {
			return
		}
		g := ensure(l, *binding, snapshot)
		if test {
			g.TestFiles = append(g.TestFiles, file)
		} else {
			g.SourceFiles = append(g.SourceFiles, file)
		}
	}
	isSource := map[string]bool{}
	for _, name := range sources {
		isSource[name] = true
	}
	for _, c := range changes {
		if !isSource[c.Path] {
			continue
		}
		if c.Status != "deleted" {
			add(after, c.Path, c.Path, false, "after")
		}
		if c.Status != "added" {
			old := c.Path
			if c.OldPath != "" {
				old = c.OldPath
			}
			add(before, old, c.Path, false, "before")
		}
	}
	for _, name := range tests {
		add(after, name, name, true, "after")
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]ComponentImpact, 0, len(keys))
	for _, key := range keys {
		g := groups[key]
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
