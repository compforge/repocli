// Package analysis coordinates repository snapshots, change impact, and ownership.
// It does not depend on the command-line framework.
package analysis

import (
	"bytes"
	"context"
	"io"
	"os"
	"sort"

	"github.com/compforge/quality-harness/sdks/go/common"
	"github.com/compforge/repocli/internal/diff"
	"github.com/compforge/repocli/internal/git"
	"github.com/compforge/repocli/internal/impact"
	"github.com/compforge/repocli/internal/project"
)

type Request struct {
	Repository string
	Base       string
	PatchFile  string
	TestDirs   []string
	Stdin      io.Reader
}

type Report struct {
	impact.Result
	Checkout   string                    `json:"checkout"`
	Repository *common.Repository        `json:"repository"`
	Base       string                    `json:"base"`
	Input      string                    `json:"input"`
	Components []project.ComponentImpact `json:"components"`
}

// Analyze compares repository snapshots and attaches source ownership.
func Analyze(ctx context.Context, req Request) (Report, error) {
	r, err := git.Open(ctx, req.Repository)
	if err != nil {
		return Report{}, err
	}
	ref, err := r.Resolve(ctx, req.Base)
	if err != nil {
		return Report{}, err
	}
	before, err := r.Base(ctx, ref)
	if err != nil {
		return Report{}, err
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
				return Report{}, err
			}
			defer f.Close()
			reader = f
		}
		changes, err = diff.Parse(reader)
		if err != nil {
			return Report{}, err
		}
		after.Files, err = diff.Apply(before.Files, changes)
		if err != nil {
			return Report{}, err
		}
	} else {
		patch, err := r.Patch(ctx, ref)
		if err != nil {
			return Report{}, err
		}
		changes, err = diff.Parse(bytes.NewReader(patch))
		if err != nil {
			return Report{}, err
		}
		var untracked []string
		after, untracked, err = r.Working(ctx)
		if err != nil {
			return Report{}, err
		}
		changed := map[string]bool{}
		for _, c := range changes {
			changed[c.Path] = true
		}
		for _, name := range untracked {
			if !changed[name] {
				changes = append(changes, diff.Added(name, after.Files[name]))
			}
		}
		sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	}
	issues := append(before.Issues, after.Issues...)
	result, err := impact.Analyze(ctx, impact.Request{Before: before.Files, After: after.Files, Changes: changes, TestDirs: req.TestDirs, Issues: issues})
	if err != nil {
		return Report{}, err
	}
	origin, err := r.Origin(ctx)
	if err != nil {
		return Report{}, err
	}
	oldLayout, err := project.Load(before.Files, origin)
	if err != nil {
		return Report{}, err
	}
	newLayout, err := project.Load(after.Files, origin)
	if err != nil {
		return Report{}, err
	}
	return Report{Result: result, Checkout: r.Root, Repository: newLayout.Repository, Base: ref, Input: input,
		Components: project.Group(oldLayout, newLayout, changes, result.SourceFiles, result.TestFiles)}, nil
}
