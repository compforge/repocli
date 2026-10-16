package analysis

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	shared "github.com/compforge/codegraph"
	"github.com/compforge/go-stdx/timeline"
	"github.com/compforge/repocli/toolkit/go/internal/git"
	"github.com/compforge/repocli/toolkit/go/internal/observation"
)

// selectGraphMaterials owns directory entry as well as document admission.
// +spec=`Directory scope, tags and ancestor ignore rules are checked before ReadDirectory; only entered directories contribute rules`
// +link=repo://docs/codegraph.md
func selectGraphMaterials(ctx context.Context, source GraphSource, matcher *shared.TagMatcher, opts GraphOptions) (selected []SourceFile, rules map[string]string, err error) {
	ref, _ := timeline.StageFromContext(ctx)
	ctx, stage := observation.Begin(ctx, ref.TimelineID, "graph.materials", timeline.WithParent(ref.StageID))
	entered := 0
	defer func() {
		stage.End(err, timeline.WithEndAttributes(
			timeline.Attribute{Key: "enteredDirectories", Value: entered},
			timeline.Attribute{Key: "admittedFiles", Value: len(selected)},
			timeline.Attribute{Key: "ignoreFiles", Value: len(rules)}))
	}()
	for _, scope := range opts.CodeGraph.Scope {
		if !fs.ValidPath(scope) {
			return nil, nil, fmt.Errorf("invalid graph scope %q", scope)
		}
	}
	inScope := func(name string, directory bool) bool {
		if len(opts.CodeGraph.Scope) == 0 {
			return true
		}
		for _, scope := range opts.CodeGraph.Scope {
			if scope == "." || name == scope || strings.HasPrefix(name, scope+"/") || directory && (name == "." || strings.HasPrefix(scope, name+"/")) {
				return true
			}
		}
		return false
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
	rules = map[string]string{}
	var ignore git.IgnoreMatcher
	defer ignore.Close()
	var ruleBytes int64
	frontier := []string{"."}
	for len(frontier) != 0 {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		var dirs, names []string
		for _, name := range frontier {
			if !inScope(name, true) || excludedTags(matcher.Match(name)) {
				continue
			}
			dirs = append(dirs, name)
			if name != "." {
				names = append(names, name+"/")
			}
		}
		ignored, err := ignore.Ignored(ctx, names)
		if err != nil {
			return nil, nil, fmt.Errorf("select graph directories: %w", err)
		}
		var children []string
		var documents []SourceFile
		names = nil
		for _, name := range dirs {
			if ignored[name+"/"] {
				continue
			}
			entries, err := source.ReadDirectory(ctx, name)
			if err != nil {
				return nil, nil, fmt.Errorf("read graph directory %s: %w", name, err)
			}
			entered++
			var nextDirs []string
			for _, child := range entries.Directories {
				if !fs.ValidPath(child) || child == "." || path.Dir(child) != name {
					return nil, nil, fmt.Errorf("invalid graph directory child %q under %s", child, name)
				}
				if inScope(child, true) && !excludedTags(matcher.Match(child)) {
					nextDirs = append(nextDirs, child)
				}
			}
			var nextDocs []SourceFile
			var rule *SourceFile
			for _, file := range entries.Documents {
				if !fs.ValidPath(file.Path) || file.Path == "." || path.Dir(file.Path) != name {
					return nil, nil, fmt.Errorf("invalid graph source path %q under %s", file.Path, name)
				}
				if path.Base(file.Path) == ".gitignore" {
					f := file
					rule = &f
				}
				tags := matcher.DocumentTags(shared.Document{Path: file.Path})
				if !inScope(file.Path, false) || excludedTags(tags) || (opts.Select != nil && !opts.Select(file.Path, tags)) {
					continue
				}
				nextDocs = append(nextDocs, file)
			}
			if rule != nil && (len(nextDirs) != 0 || len(nextDocs) != 0) {
				if rule.Size < 0 || rule.Size > opts.CodeGraph.MaxDocumentBytes {
					return nil, nil, fmt.Errorf("snapshot ignore file %s exceeds byte limit", rule.Path)
				}
				if ruleBytes+rule.Size > opts.CodeGraph.MaxSourceBytes {
					return nil, nil, fmt.Errorf("snapshot ignore rules exceed total byte limit at %s", rule.Path)
				}
				data, err := source.ReadSource(ctx, rule.Path)
				if err != nil {
					return nil, nil, fmt.Errorf("read snapshot ignore file %s: %w", rule.Path, err)
				}
				if int64(len(data)) > opts.CodeGraph.MaxDocumentBytes || ruleBytes+int64(len(data)) > opts.CodeGraph.MaxSourceBytes {
					return nil, nil, fmt.Errorf("snapshot ignore file %s exceeds byte limit", rule.Path)
				}
				if err := ignore.Add(ctx, rule.Path, []byte(data)); err != nil {
					return nil, nil, fmt.Errorf("load snapshot ignore file %s: %w", rule.Path, err)
				}
				rules[rule.Path] = data
				ruleBytes += int64(len(data))
			}
			children = append(children, nextDirs...)
			documents = append(documents, nextDocs...)
			for _, file := range nextDocs {
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
