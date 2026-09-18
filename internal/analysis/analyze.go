// Package analysis coordinates repository snapshots, change impact, and ownership.
// It does not depend on the command-line framework.
package analysis

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/compforge/quality-harness/sdks/go/common"
	"github.com/compforge/repocli/internal/diff"
	"github.com/compforge/repocli/internal/git"
	"github.com/compforge/repocli/internal/impact"
	"github.com/compforge/repocli/internal/project"
)

type Request struct {
	Repository   string
	Head         string
	Staged       bool
	ChangedFiles []string
	Mode         string
	Base         string
	PatchFile    string
	TestDirs     []string
	Stdin        io.Reader
}

type Diagnostic struct {
	Code    string `json:"code"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

type Report struct {
	impact.Result
	Head        string                    `json:"head,omitempty"`
	Snapshot    string                    `json:"snapshot"`
	Complete    bool                      `json:"complete"`
	Diagnostics []Diagnostic              `json:"diagnostics"`
	ImpactMode  string                    `json:"impactMode"`
	Checkout    string                    `json:"checkout"`
	Repository  *common.Repository        `json:"repository"`
	Base        string                    `json:"base"`
	Input       string                    `json:"input"`
	Components  []project.ComponentImpact `json:"components"`
}

// Analyze compares repository snapshots and attaches source ownership.
func Analyze(ctx context.Context, req Request) (Report, error) {
	for _, name := range req.ChangedFiles {
		if !diff.ValidPath(name) {
			return Report{}, fmt.Errorf("invalid changed file: %q", name)
		}
	}
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
	head := ""
	if req.Head != "" {
		head, err = r.Resolve(ctx, req.Head)
		if err != nil {
			return Report{}, err
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
			return Report{}, err
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
		patch, err := r.ComparisonPatch(ctx, ref, head, req.Staged)
		if err != nil {
			return Report{}, err
		}
		changes, err = diff.Parse(bytes.NewReader(patch))
		if err != nil {
			return Report{}, err
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
	result, err := impact.Analyze(ctx, impact.Request{Before: before.Files, After: after.Files, Changes: changes, TestDirs: req.TestDirs, Issues: issues, Mode: req.Mode})
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
	diagnostics := []Diagnostic{}
	for _, message := range issues {
		diagnostics = append(diagnostics, diagnostic("snapshot_incomplete", message))
	}
	for _, message := range result.FallbackReasons {
		diagnostics = append(diagnostics, diagnostic("impact_uncertain", message))
	}
	if head == "" && req.PatchFile == "" {
		var latest git.Snapshot
		if req.Staged {
			latest, err = r.Staged(ctx)
		} else {
			latest, _, err = r.Working(ctx)
		}
		if err != nil {
			return Report{}, err
		}
		if observed.Digest() != after.Digest() || after.Digest() != latest.Digest() {
			diagnostics = append(diagnostics, Diagnostic{Code: "snapshot_changed", Message: "repository contents changed during analysis"})
			result.Scope = "fallback"
		}
	}
	mode := req.Mode
	if mode == "" {
		mode = "symbol"
	}
	return Report{Head: head, Snapshot: after.Digest(), Complete: len(diagnostics) == 0, Diagnostics: diagnostics, ImpactMode: mode, Result: result, Checkout: r.Root, Repository: newLayout.Repository, Base: ref, Input: input,
		Components: project.Group(oldLayout, newLayout, changes, result.SourceFiles, result.TestFiles)}, nil
}

// Snapshot and impact adapters prefix file-local issues with their repository path.
// Preserve the human message while exposing that location to structured consumers.
func diagnostic(code, message string) Diagnostic {
	d := Diagnostic{Code: code, Message: message}
	if name, _, ok := strings.Cut(message, ": "); ok && diff.ValidPath(name) {
		d.Path = name
	}
	return d
}
