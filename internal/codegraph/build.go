package codegraph

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"

	shared "github.com/compforge/codegraph"
	"golang.org/x/mod/modfile"
)

// BuildOptions supplies a single captured version and bounded extraction needs.
// Files is a read-only catalog, not an instruction to parse every source.
type BuildOptions struct {
	Files     map[string][]byte
	Resources map[string][]byte // Captured configuration resources; never source/candidate catalog.
	Gitlinks  map[string]bool
	Kinds     []Kind
	MaxDepth  int
	MaxFiles  int
}

type BuildRequest struct {
	BuildOptions
	FilesToExpand []string
}

type BuildResult struct {
	Graph       *Graph
	ParsedFiles []string
	Issues      []Issue
	Sources     map[string]Source
}

type frontier struct {
	file  string
	depth int
}

// Builder starts with an empty graph. Add explores only the supplied file and
// its bounded dependency workset within one captured version.
type Builder struct {
	req          BuildOptions
	result       BuildResult
	enabled      map[Kind]bool
	resolver     *resolver
	goFiles      map[string][]string
	configIssues []Issue
	depths       map[string]int
	dependencies map[string][]string
	visited      map[string]bool
	workset      map[string]shared.Document
	sourceGraph  *shared.Graph
	sources      map[string]sharedSourceResult
}

func NewBuilder(req BuildOptions) (*Builder, error) {
	if req.MaxDepth < 0 || req.MaxFiles <= 0 {
		return nil, fmt.Errorf("codegraph requires a nonnegative depth and a positive file limit")
	}
	enabled := map[Kind]bool{}
	for _, kind := range req.Kinds {
		enabled[kind] = true
	}
	// Catalog metadata supports resolution without parsing unrelated source.
	modules := map[string]string{}
	var configIssues []Issue
	goFiles := map[string][]string{}
	for name, data := range req.Files {
		if ignoredDependency(name) {
			continue
		}
		if Language(name) == "go" {
			goFiles[path.Dir(name)] = append(goFiles[path.Dir(name)], name)
		}
		if path.Base(name) != "go.mod" {
			continue
		}
		m, err := modfile.Parse(name, data, nil)
		if err != nil || m.Module == nil {
			configIssues = append(configIssues, Issue{Path: name, Kind: Imports, Code: "invalid_config", Message: "cannot resolve Go module"})
			continue
		}
		modules[path.Dir(name)] = m.Module.Mod.Path
		for _, replacement := range m.Replace {
			if replacement.New.Version == "" {
				configIssues = append(configIssues, Issue{Path: name, Kind: Imports, Code: "unsupported_config", Message: "local replace directives are not resolved"})
			}
		}
	}
	resolver := newResolver(req.Files, modules, req.Resources, req.Gitlinks)
	configIssues = append(configIssues, resolver.configIssues...)

	sourceGraph, err := shared.New("repocli", shared.Options{MaxFiles: req.MaxFiles})
	if err != nil {
		return nil, err
	}
	return &Builder{req: req, result: BuildResult{Graph: New()}, enabled: enabled, resolver: resolver,
		goFiles: goFiles, configIssues: configIssues, depths: map[string]int{}, dependencies: map[string][]string{}, visited: map[string]bool{},
		workset: map[string]shared.Document{}, sourceGraph: sourceGraph, sources: map[string]sharedSourceResult{}}, nil
}

func (b *Builder) link(from, to string, kind Kind, file string, line int) {
	if b.enabled[kind] {
		b.result.Graph.AddRelation(Relation{From: from, To: to, Kind: kind, File: file, Line: line})
	}
}

// Add is idempotent for parsed files. Shallower additions can complete a previous
// depth-limited frontier without re-parsing already visited sources.
func (b *Builder) Add(ctx context.Context, files ...string) error {
	req, result, g := b.req, &b.result, b.result.Graph
	resolver, enabled, goFiles := b.resolver, b.enabled, b.goFiles
	visited, link := b.visited, b.link
	var documents []shared.Document
	var queue []frontier
	parent := ""
	enqueue := func(file string, depth int) {
		if parent != "" {
			b.dependencies[parent] = append(b.dependencies[parent], file)
		}
		if previous, ok := b.depths[file]; ok && previous <= depth {
			return
		}
		b.depths[file] = depth
		queue = append(queue, frontier{file, depth})
	}
	// Register every root before traversing dependencies, so root priority and
	// depth do not depend on which candidate happened to be added first.
	for _, file := range unique(files) {
		enqueue(file, 0)
	}
	for i := 0; i < len(queue); i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		item := queue[i]
		name := item.file
		parent = ""
		if visited[name] {
			for _, dependency := range unique(b.dependencies[name]) {
				enqueue(dependency, item.depth+1)
			}
			continue
		}
		if req.Gitlinks[name] {
			g.AddNode(Node{ID: name, Kind: "file", File: name})
			continue
		}
		data, exists := req.Files[name]
		if !exists {
			continue
		} // A root may exist only on the other comparison side.
		if ignoredDependency(name) {
			result.Issues = append(result.Issues, Issue{Path: name, Code: "boundary_unavailable", Message: "dependency is outside source boundary"})
			continue
		}
		if item.depth > req.MaxDepth || len(result.ParsedFiles) >= req.MaxFiles {
			result.Issues = append(result.Issues, Issue{Path: name, Code: "expansion_limit", Message: "local graph expansion limit reached"})
			continue
		}
		// A later Add may reach a previously depth-limited file directly.
		result.Issues = slices.DeleteFunc(result.Issues, func(issue Issue) bool { return issue.Path == name && issue.Code == "expansion_limit" })
		visited[name] = true
		parent = name
		g.AddNode(Node{ID: name, Kind: "file", File: name})
		language := Language(name)
		if language == "" {
			continue
		}
		document := shared.Document{Path: name, Content: data}
		b.workset[name] = document
		documents = append(documents, document)
		result.ParsedFiles = append(result.ParsedFiles, name)
		g.AddNode(Node{ID: ModuleID(name), Kind: "module", File: name})
		// Shared extraction is cached by content identity: the AddDocuments
		// call below reuses these facts instead of parsing the file again.
		sfacts, err := b.sourceGraph.Extract(ctx, document)
		var imports []shared.FactImport
		var exports map[string]string
		var statements []shared.Statement
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			result.Issues = append(result.Issues, Issue{Path: name, From: name, Code: "parse_error", Message: err.Error()})
		} else {
			for _, issue := range sfacts.Issues {
				result.Issues = append(result.Issues, Issue{Path: name, From: name, Kind: issueKind(issue.Code), Code: issue.Code, Line: issue.Location.Line, Message: issue.Message})
			}
			imports, exports, statements = sfacts.Imports, sfacts.Exports, sfacts.Statements
		}
		for public, local := range exports {
			link(SymbolID(name, public), SymbolID(name, local), Reexports, name, 0)
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
		var resolved []resolvedImport
		if language == "python" {
			var issues []Issue
			resolved, issues = resolver.pythonImports(ctx, name, statements)
			result.Issues = append(result.Issues, issues...)
		} else {
			for _, imp := range imports {
				deps, issue := resolver.resolve(name, language, imp)
				if issue != nil {
					issue.Path = name
					issue.From = name
					issue.Line = imp.Location.Line
					result.Issues = append(result.Issues, *issue)
					for _, target := range issue.Targets {
						enqueue(target, item.depth+1)
					}
				} else {
					resolved = append(resolved, resolvedImport{reference: imp, targets: deps})
				}
			}
		}
		for _, resolution := range resolved {
			imp, deps := resolution.reference, resolution.targets
			dependency := func(from, to string) {
				g.AddRelation(Relation{From: from, To: to, Kind: Imports, File: name, Line: imp.Location.Line, Confidence: resolution.confidence, Basis: resolution.basis})
			}

			for _, dep := range deps {
				// Record the known boundary edge even when expansion is limited.
				dependency(name, dep)
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
					dependency(name, ModuleID(dep))
				} else {
					for _, symbol := range named {
						dependency(name, SymbolID(dep, symbol))
					}
				}
				enqueue(dep, item.depth+1)
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	if len(documents) != 0 {
		// Shared CodeGraph resolves the complete accumulated batch. Never build a
		// separate graph for each file, or rebuild once per candidate root.
		report, err := b.sourceGraph.AddDocuments(ctx, documents...)
		if err != nil {
			return fmt.Errorf("build source workset: %w", err)
		}
		b.sources = projectSources(b.sourceGraph, report)
	}
	return nil
}

// Build is a batch convenience for callers that already have their explicit
// exploration files. It uses the same incremental builder as Add.
func Build(ctx context.Context, req BuildRequest) (BuildResult, error) {
	builder, err := NewBuilder(req.BuildOptions)
	if err != nil {
		return BuildResult{}, err
	}
	if err := builder.Add(ctx, req.FilesToExpand...); err != nil {
		return builder.Result(), err
	}
	return builder.Result(), ctx.Err()
}

// issueKind classifies shared extraction diagnostics into repocli's relation
// kinds; import-resolution gaps belong to the Imports relation evidence.
func issueKind(code string) Kind {
	switch code {
	case "dynamic_import", "context_limit", "unsupported_resource":
		return Imports
	default:
		return ""
	}
}
