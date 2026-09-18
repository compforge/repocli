package codegraph

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/compforge/repocli/internal/syntax"
	"golang.org/x/mod/modfile"
)

// Issue describes an unresolved relation or a boundary reached during building.
type Issue struct {
	Path, Message string
	Configuration bool
}

// BuildRequest supplies the available version and the caller's exploration roots.
// Files is a read-only catalog, not an instruction to parse every source.
type BuildRequest struct {
	Files      map[string][]byte
	Roots      []string
	Candidates []string
	Gitlinks   map[string]bool
	Kinds      []Kind
	MaxDepth   int
	MaxFiles   int
	Analyzer   *syntax.Analyzer
}

type BuildResult struct {
	Graph       *Graph
	ParsedFiles []string
	Issues      []Issue
}

type frontier struct {
	file  string
	depth int
}

// Build extracts roots and candidates, then follows only resolved local imports.
// +spec=`No repository-wide source parse; unresolved targets never create guessed edges`
// +why=`A caller selects candidate scope while the builder remains independent of that caller's purpose`
func Build(ctx context.Context, req BuildRequest) (BuildResult, error) {
	if req.MaxDepth < 0 || req.MaxFiles <= 0 {
		return BuildResult{}, fmt.Errorf("codegraph requires a nonnegative depth and a positive file limit")
	}
	result := BuildResult{Graph: New()}
	g := result.Graph
	analyzer := req.Analyzer
	if analyzer == nil {
		analyzer = &syntax.Analyzer{}
	}
	enabled := map[Kind]bool{}
	for _, kind := range req.Kinds {
		enabled[kind] = true
	}
	link := func(from, to string, kind Kind, file string, line int) {
		if enabled[kind] {
			g.AddRelation(Relation{From: from, To: to, Kind: kind, File: file, Line: line})
		}
	}
	// Catalog metadata supports resolution without parsing unrelated source.
	modules := map[string]string{}
	var configIssues []Issue
	goFiles := map[string][]string{}
	for name, data := range req.Files {
		if ignoredDependency(name) {
			continue
		}
		if syntax.Language(name) == "go" {
			goFiles[path.Dir(name)] = append(goFiles[path.Dir(name)], name)
		}
		if path.Base(name) != "go.mod" {
			continue
		}
		m, err := modfile.Parse(name, data, nil)
		if err != nil || m.Module == nil {
			configIssues = append(configIssues, Issue{Path: name, Message: "cannot resolve Go module", Configuration: true})
			continue
		}
		modules[path.Dir(name)] = m.Module.Mod.Path
		for _, replacement := range m.Replace {
			if replacement.New.Version == "" {
				configIssues = append(configIssues, Issue{Path: name, Message: "local replace directives are not resolved", Configuration: true})
			}
		}
	}
	resolver := newResolver(req.Files, modules)
	resolver.gitlinks = req.Gitlinks
	configIssues = append(configIssues, resolver.configIssues...)
	var queue []frontier
	queued := map[string]bool{}
	enqueue := func(file string, depth int) {
		if !queued[file] {
			queued[file] = true
			queue = append(queue, frontier{file, depth})
		}
	}
	for _, file := range unique(append(append([]string{}, req.Roots...), req.Candidates...)) {
		enqueue(file, 0)
	}
	visited := map[string]bool{}
	for i := 0; i < len(queue); i++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		item := queue[i]
		name := item.file
		if req.Gitlinks[name] {
			g.AddNode(Node{ID: name, Kind: "file", File: name})
			continue
		}
		data, exists := req.Files[name]
		if !exists {
			continue
		} // A root may exist only on the other comparison side.
		if ignoredDependency(name) {
			result.Issues = append(result.Issues, Issue{Path: name, Message: "dependency is outside source boundary"})
			continue
		}
		if item.depth > req.MaxDepth || len(result.ParsedFiles) >= req.MaxFiles {
			result.Issues = append(result.Issues, Issue{Path: name, Message: "local graph expansion limit reached"})
			continue
		}
		visited[name] = true
		g.AddNode(Node{ID: name, Kind: "file", File: name})
		language := syntax.Language(name)
		if language == "" {
			continue
		}
		facts := analyzer.Analyze(ctx, name, data)
		result.ParsedFiles = append(result.ParsedFiles, name)
		g.AddNode(Node{ID: ModuleID(name), Kind: "module", File: name})
		for _, s := range facts.Symbols {
			id := SymbolID(name, s.QualifiedName)
			g.AddNode(Node{ID: id, Kind: "symbol", File: name, Name: s.QualifiedName, StartLine: s.StartLine, EndLine: s.EndLine})
			parent := name
			if s.Parent != "" {
				parent = SymbolID(name, s.Parent)
			}
			link(parent, id, Contains, name, s.StartLine)
		}
		for _, issue := range facts.Issues {
			result.Issues = append(result.Issues, Issue{Path: name, Message: issue})
		}
		for public, local := range facts.Exports {
			link(SymbolID(name, public), SymbolID(name, local), Reexports, name, 0)
		}
		for _, call := range facts.Calls {
			link(SymbolID(name, call.Caller), SymbolID(name, call.Callee), Calls, name, call.Line)
		}
		for _, root := range facts.SearchPaths {
			captured := false
			for file := range req.Files {
				if root == "." || strings.HasPrefix(file, root+"/") {
					captured = true
					break
				}
			}
			if !captured {
				result.Issues = append(result.Issues, Issue{Path: name, Message: "Python search path is outside captured contents: " + root})
			}
		}
		if language == "go" && enabled[PackageMember] {
			// Go's implicit package scope is a language rule. The _test suffix
			// separates test compilation units; it is not candidate selection.
			dir := path.Dir(name)
			pkg := "package:" + dir
			g.AddNode(Node{ID: pkg, Kind: "module", File: dir})
			link(name, pkg, PackageMember, name, 0)
			if strings.HasSuffix(name, "_test.go") {
				group := "tests:" + dir
				g.AddNode(Node{ID: group, Kind: "module", File: dir})
				link(group, name, PackageMember, name, 0)
				link(name, group, PackageMember, name, 0)
			} else {
				link(pkg, name, PackageMember, name, 0)
			}
			for _, member := range unique(goFiles[dir]) {
				if !strings.HasSuffix(member, "_test.go") {
					enqueue(member, item.depth)
				}
			}
		}
		if !enabled[Imports] {
			continue
		}
		for _, imp := range facts.Imports {
			deps, issue := resolver.resolve(name, language, imp)
			if issue != "" {
				result.Issues = append(result.Issues, Issue{Path: name, Message: issue})
				continue
			}
			for _, dep := range deps {
				// Record the known boundary edge even when expansion is limited.
				link(name, dep, Imports, name, imp.Line)
				if strings.HasPrefix(dep, "package:") {
					dir := strings.TrimPrefix(dep, "package:")
					for _, member := range unique(goFiles[dir]) {
						if !strings.HasSuffix(member, "_test.go") {
							enqueue(member, item.depth+1)
						}
					}
					continue
				}
				named := imp.Names
				if language == "python" && strings.TrimSuffix(path.Base(dep), ".py") == path.Base(strings.ReplaceAll(imp.Path, ".", "/")) {
					named = nil
				}
				if req.Gitlinks[dep] {
					enqueue(dep, item.depth+1)
					continue
				}
				if len(named) == 0 {
					link(name, ModuleID(dep), Imports, name, imp.Line)
				} else {
					for _, symbol := range named {
						link(name, SymbolID(dep, symbol), Imports, name, imp.Line)
					}
				}
				enqueue(dep, item.depth+1)
			}
		}
	}
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
			if _, exists := req.Files[parent]; !exists {
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
			result.Issues = append(result.Issues, issue)
		}
	}
	result.ParsedFiles = unique(result.ParsedFiles)
	return result, nil
}
