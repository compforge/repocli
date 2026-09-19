package codegraph

import (
	"context"
	"go/parser"
	"go/token"
	"path"
	"strconv"
	"strings"
)

// ScopeCandidates conservatively narrows caller-supplied files before Add.
// It neither expands the graph nor asserts a relation: graph queries still own
// evidence. Only Go-only entry sets have a cheap package-level exclusion rule.
func (b *Builder) ScopeCandidates(ctx context.Context, entryFiles, candidates []string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(entryFiles) == 0 || len(candidates) == 0 || len(b.configIssues) > 0 {
		return candidates, nil
	}
	affected := map[string]bool{}
	queue := []string{}
	for _, name := range entryFiles {
		if Language(name) != "go" {
			return candidates, nil
		}
		dir := path.Dir(name)
		if !affected[dir] {
			affected[dir] = true
			queue = append(queue, dir)
		}
	}
	hasGoCandidate := false
	for _, name := range candidates {
		if Language(name) == "go" {
			hasGoCandidate = true
			break
		}
	}
	if !hasGoCandidate {
		return candidates, nil
	}

	// ImportsOnly stops before declarations/bodies. Scan captured headers rather
	// than parsing every candidate's full AST, or invoking go list on target code.
	reverse := map[string]map[string]bool{}
	for dir, files := range b.goFiles {
		for _, name := range files {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			header, err := parser.ParseFile(token.NewFileSet(), name, b.req.Files[name], parser.ImportsOnly)
			if err != nil {
				return candidates, nil
			}
			for _, imp := range header.Imports {
				spec, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					return candidates, nil
				}
				targets, issue := b.resolver.goImport(spec)
				if issue != nil {
					return candidates, nil
				}
				for _, target := range targets {
					dependency := strings.TrimPrefix(target, "package:")
					if len(b.goFiles[dependency]) == 0 {
						return candidates, nil // A known package outside the expansion boundary is still unknown.
					}
					if reverse[dependency] == nil {
						reverse[dependency] = map[string]bool{}
					}
					reverse[dependency][dir] = true
				}
			}
		}
	}
	// Test imports participate too: an external test package can consume a changed
	// package without any corresponding import in its production sources.
	for i := 0; i < len(queue); i++ {
		for importer := range reverse[queue[i]] {
			if !affected[importer] {
				affected[importer] = true
				queue = append(queue, importer)
			}
		}
	}
	selected := make([]string, 0, len(candidates))
	for _, name := range candidates {
		if Language(name) != "go" || affected[path.Dir(name)] {
			selected = append(selected, name)
		}
	}
	return selected, nil
}
