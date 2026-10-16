package analysis

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"sort"

	shared "github.com/compforge/codegraph"
	"github.com/compforge/go-stdx/timeline"
	"github.com/compforge/repocli/toolkit/go/internal/git"
	"github.com/compforge/repocli/toolkit/go/internal/observation"
)

type graphDirectory struct {
	path      string
	documents []SourceFile
	children  []*graphDirectory
}

// selectGraphMaterials walks only directories containing selected documents.
// +spec=`Directory exclusion precedes its ignore-file read; admitted directories load their rules before testing children`
// +why=`An excluded subtree cannot contribute ignore rules or consume graph material budgets`
// +link=repo://docs/codegraph.md
func selectGraphMaterials(ctx context.Context, source GraphSource, files []SourceFile, matcher *shared.TagMatcher, opts GraphOptions) (selected []SourceFile, rules map[string]string, err error) {
	ref, _ := timeline.StageFromContext(ctx)
	ctx, stage := observation.Begin(ctx, ref.TimelineID, "graph.materials", timeline.WithParent(ref.StageID))
	defer func() {
		stage.End(err, timeline.WithEndAttributes(
			timeline.Attribute{Key: "catalogFiles", Value: len(files)},
			timeline.Attribute{Key: "admittedFiles", Value: len(selected)},
			timeline.Attribute{Key: "ignoreFiles", Value: len(rules)}))
	}()
	catalog := make(map[string]SourceFile, len(files))
	directories := map[string]*graphDirectory{".": {path: "."}}
	var directory func(string) *graphDirectory
	directory = func(name string) *graphDirectory {
		if d := directories[name]; d != nil {
			return d
		}
		d := &graphDirectory{path: name}
		directories[name] = d
		parent := directory(path.Dir(name))
		parent.children = append(parent.children, d)
		return d
	}
	var excluded []shared.Tag
	if opts.CodeGraph.Admission != nil {
		excluded = opts.CodeGraph.Admission.ExcludedTags
	}
	excludedTags := func(tags []shared.Tag) bool {
		for _, tag := range tags {
			if slices.Contains(excluded, tag) {
				return true
			}
		}
		return false
	}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if !fs.ValidPath(file.Path) || file.Path == "." {
			return nil, nil, fmt.Errorf("invalid graph source path %q", file.Path)
		}
		catalog[file.Path] = file
		tags := matcher.DocumentTags(shared.Document{Path: file.Path})
		if matcher.ExcludedBy(shared.Document{Path: file.Path}, excluded) != "" || (opts.Select != nil && !opts.Select(file.Path, tags)) {
			continue
		}
		d := directory(path.Dir(file.Path))
		d.documents = append(d.documents, file)
	}
	for _, d := range directories {
		sort.Slice(d.children, func(i, j int) bool { return d.children[i].path < d.children[j].path })
	}
	rules = map[string]string{}
	var ignore git.IgnoreMatcher
	defer ignore.Close()
	var ruleBytes int64
	frontier := []*graphDirectory{directories["."]}
	for len(frontier) != 0 {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		var dirs []*graphDirectory
		var names []string
		for _, d := range frontier {
			if excludedTags(matcher.Match(d.path)) {
				continue
			}
			dirs = append(dirs, d)
			if d.path != "." {
				names = append(names, d.path+"/")
			}
		}
		ignored, err := ignore.Ignored(ctx, names)
		if err != nil {
			return nil, nil, fmt.Errorf("select graph directories: %w", err)
		}
		var children []*graphDirectory
		var documents []SourceFile
		names = nil
		for _, d := range dirs {
			if ignored[d.path+"/"] {
				continue
			}
			rule := path.Join(d.path, ".gitignore")
			if file, exists := catalog[rule]; exists && (len(d.documents) != 0 || len(d.children) != 0) {
				if file.Size < 0 || file.Size > opts.CodeGraph.MaxDocumentBytes {
					return nil, nil, fmt.Errorf("snapshot ignore file %s exceeds byte limit", rule)
				}
				if ruleBytes+file.Size > opts.CodeGraph.MaxSourceBytes {
					return nil, nil, fmt.Errorf("snapshot ignore rules exceed total byte limit at %s", rule)
				}
				data, err := source.ReadSource(ctx, rule)
				if err != nil {
					return nil, nil, fmt.Errorf("read snapshot ignore file %s: %w", rule, err)
				}
				if int64(len(data)) > opts.CodeGraph.MaxDocumentBytes || ruleBytes+int64(len(data)) > opts.CodeGraph.MaxSourceBytes {
					return nil, nil, fmt.Errorf("snapshot ignore file %s exceeds byte limit", rule)
				}
				if err := ignore.Add(ctx, rule, []byte(data)); err != nil {
					return nil, nil, fmt.Errorf("load snapshot ignore file %s: %w", rule, err)
				}
				rules[rule] = data
				ruleBytes += int64(len(data))
			}
			children = append(children, d.children...)
			documents = append(documents, d.documents...)
			for _, file := range d.documents {
				names = append(names, file.Path)
			}
		}
		ignored, err = ignore.Ignored(ctx, names)
		if err != nil {
			return nil, nil, fmt.Errorf("select graph documents: %w", err)
		}
		for _, file := range documents {
			if !ignored[file.Path] {
				selected = append(selected, file)
			}
		}
		frontier = children
	}
	return selected, rules, nil
}
