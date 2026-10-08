package analysis

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/compforge/repocli/toolkit/go/internal/diff"
	"github.com/compforge/repocli/toolkit/go/internal/git"
)

type comparison struct {
	repo                    *git.Repository
	base, head, input       string
	before, after, observed git.Snapshot
	changes                 []diff.Change
	issues                  []string
	skipped                 map[string]string
	gitlinks                map[string]bool
}

// captureComparison is shared by impact and Unit analysis so input selection,
// patch reconstruction and mutable-snapshot checks cannot diverge.
func captureComparison(ctx context.Context, req Request) (*comparison, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if req.Base == "" && !req.EmptyBase {
		req.Base = "HEAD"
	}
	for _, name := range req.ChangedFiles {
		if !diff.ValidPath(name) {
			return nil, fmt.Errorf("invalid changed file: %q", name)
		}
	}
	r, err := git.Open(ctx, req.Repository)
	if err != nil {
		return nil, err
	}
	var ref string
	var before git.Snapshot
	if req.EmptyBase {
		before = git.Snapshot{Files: map[string][]byte{}}
	} else {
		ref, err = r.Resolve(ctx, req.Base)
		if err != nil {
			return nil, err
		}
		before, err = r.Base(ctx, ref)
		if err != nil {
			return nil, err
		}
	}
	head := ""
	if req.Head != "" {
		head, err = r.Resolve(ctx, req.Head)
		if err != nil {
			return nil, err
		}
	}
	var observed git.Snapshot
	if req.PatchFile == "" && head == "" {
		if req.Staged {
			observed, err = r.Staged(ctx)
		} else {
			observed, _, err = r.Working(ctx)
		}
		if err != nil {
			return nil, err
		}
	}
	var after git.Snapshot
	var changes []diff.Change
	input := "working_tree"
	if req.PatchFile != "" {
		input = "patch"
		reader := req.Stdin
		if req.PatchFile != "-" {
			f, err := os.Open(req.PatchFile)
			if err != nil {
				return nil, err
			}
			defer f.Close()
			reader = f
		}
		changes, err = diff.Parse(reader)
		if err != nil {
			return nil, err
		}
		if len(before.Links) > 0 || len(before.Modules) > 0 || len(before.Opaque) > 0 {
			return nil, fmt.Errorf("patch reconstruction with symlinks, submodules or large files is not supported; use working-tree, staged or commit comparison")
		}
		after.Files, err = diff.Apply(before.Files, changes)
		if err != nil {
			return nil, err
		}
	} else {
		patch, err := r.ComparisonPatch(ctx, ref, head, req.Staged)
		if err != nil {
			return nil, err
		}
		changes, err = diff.Parse(bytes.NewReader(patch))
		if err != nil {
			return nil, err
		}
		var untracked []string
		switch {
		case head != "":
			input = "commit"
			after, err = r.Base(ctx, head)
		case req.Staged:
			input = "index"
			after, err = r.Staged(ctx)
		default:
			after, untracked, err = r.Working(ctx)
		}
		if err != nil {
			return nil, err
		}
		changed := map[string]bool{}
		for _, c := range changes {
			changed[c.Path] = true
		}
		for _, name := range untracked {
			if !changed[name] {
				data := after.Files[name]
				if target, ok := after.Links[name]; ok {
					data = []byte(target)
				}
				changes = append(changes, diff.Added(name, data))
			}
		}
		sort.SliceStable(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	}
	// A gitlink is one parent-repository entry, not an instruction to analyze
	// or discover tests inside its dependency repository.
	gitlinks := map[string]bool{}
	for name := range before.Modules {
		gitlinks[name] = true
	}
	for name := range after.Modules {
		gitlinks[name] = true
	}
	skipped := map[string]string{}
	for _, snapshot := range []git.Snapshot{before, after} {
		for name := range snapshot.Links {
			skipped[name] = "symlink dependency impact is not modeled"
		}
		for name := range snapshot.Opaque {
			skipped[name] = "large-file dependency impact is not modeled"
		}
	}
	if len(req.ChangedFiles) > 0 {
		selected := map[string]bool{}
		for _, name := range req.ChangedFiles {
			selected[name] = true
		}
		filtered := []diff.Change{}
		for _, change := range changes {
			if selected[change.Path] || selected[change.OldPath] {
				filtered = append(filtered, change)
			}
		}
		changes = filtered
	}
	issues := append(append([]string{}, before.Issues...), after.Issues...)
	return &comparison{repo: r, base: ref, head: head, input: input, before: before, after: after, observed: observed, changes: changes, issues: issues, skipped: skipped, gitlinks: gitlinks}, nil
}
func (c *comparison) changed(ctx context.Context, req Request) (bool, error) {
	if c.head != "" || req.PatchFile != "" {
		return false, nil
	}
	var latest git.Snapshot
	var err error
	if req.Staged {
		latest, err = c.repo.Staged(ctx)
	} else {
		latest, _, err = c.repo.Working(ctx)
	}
	if err != nil {
		return false, err
	}
	return c.observed.Digest() != c.after.Digest() || c.after.Digest() != latest.Digest(), nil
}
