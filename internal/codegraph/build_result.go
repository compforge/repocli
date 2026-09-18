package codegraph

import (
	"cmp"
	"path"
	"slices"
	"strings"

	"github.com/compforge/repocli/internal/codegraph/internal/syntax"
)

// Result attaches resolution configuration to visited nodes. Catalog resources
// themselves never trigger source parsing or discover candidates.
func (b *Builder) Result() BuildResult {
	req, resolver, visited, configIssues, link := b.req, b.resolver, b.visited, b.configIssues, b.link
	result := BuildResult{Graph: b.result.Graph, ParsedFiles: slices.Clone(b.result.ParsedFiles), Issues: slices.Clone(b.result.Issues)}
	// Configuration applicability scopes diagnostics only. It must never be
	// confused with an evidenced source dependency by the impact consumer.
	activeConfigs := map[string]bool{}
	for config := range req.Files {
		if !strings.HasPrefix(path.Base(config), "tsconfig") || !strings.HasSuffix(config, ".json") || ignoredDependency(config) {
			continue
		}
		if visited[config] {
			activeConfigs[config] = true
		}
		dir := path.Dir(config)
		for name := range visited {
			lang := syntax.Language(name)
			if (lang == "typescript" || lang == "tsx" || lang == "javascript") && (dir == "." || strings.HasPrefix(name, dir+"/")) {
				activeConfigs[config] = true
				link(name, config, ConfigScope, config, 0)
			}
		}
	}
	var configQueue []string
	for config := range activeConfigs {
		configQueue = append(configQueue, config)
	}
	configQueue = unique(configQueue)
	for i := 0; i < len(configQueue); i++ {
		config := configQueue[i]
		for _, parent := range unique(resolver.configDependencies[config]) {
			// Missing parents remain diagnostics, not asserted dependency edges.
			if _, exists := resolver.configData(parent); !exists {
				continue
			}
			link(config, parent, ConfigExtends, config, 0)
			if !activeConfigs[parent] {
				activeConfigs[parent] = true
				configQueue = append(configQueue, parent)
			}
		}
	}
	for _, issue := range configIssues {
		relevant := activeConfigs[issue.Path] || visited[issue.Path]
		dir := path.Dir(issue.Path)
		for name := range visited {
			if dir == "." || strings.HasPrefix(name, dir+"/") {
				relevant = true
				break
			}
		}
		if relevant {
			// Module and manifest failures affect resolution for their directory.
			// Applicability is diagnostic context, not a dependency assertion.
			for name := range visited {
				if dir == "." || strings.HasPrefix(name, dir+"/") {
					link(name, issue.Path, ConfigScope, issue.Path, 0)
				}
			}
			issue.From = issue.Path
			result.Issues = append(result.Issues, issue)
		}
	}
	slices.SortFunc(result.Issues, func(a, b Issue) int {
		if c := cmp.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Line, b.Line); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Code, b.Code); c != 0 {
			return c
		}
		return cmp.Compare(a.Message, b.Message)
	})
	result.ParsedFiles = unique(result.ParsedFiles)
	return result
}

// Query evaluates a relation query over this build's graph and resolution gaps.
// It does not parse additional source or apply a test-selection policy.
func (r BuildResult) Query(entries, candidates []string, kinds []Kind) QueryResult {
	return r.Graph.Query(entries, candidates, kinds, r.Issues)
}
