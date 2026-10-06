package analysis

import (
	"context"
	"fmt"

	shared "github.com/compforge/codegraph"
	"github.com/compforge/repocli/internal/codegraph"
	"github.com/compforge/repocli/internal/git"
	"github.com/compforge/repocli/internal/project"
)

// PreparedSnapshot is command-scoped: consumers treat both its metadata and bytes
// as read-only. Refresh replaces the whole value, never just the source contents.
type PreparedSnapshot struct {
	Report    SnapshotReport
	Contents  git.Snapshot
	Manifests *shared.Graph
}

// Prepare captures the selected input and discovers its repository/component context.
// +spec=`Commands derive repository context and source bytes from the same selected snapshot`
// +why=`Preparing in the analysis layer keeps commit, index and patch inputs independent of the live working tree`
func Prepare(ctx context.Context, req SnapshotRequest) (*PreparedSnapshot, error) {
	if req.Head != "" && req.Staged {
		return nil, fmt.Errorf("head and staged are mutually exclusive")
	}
	repo, err := git.Open(ctx, req.Repository)
	if err != nil {
		return nil, err
	}
	input, head := "working_tree", ""
	if req.Head != "" {
		input = "commit"
		head, err = repo.Resolve(ctx, req.Head)
		if err != nil {
			return nil, err
		}
	} else if req.Staged {
		input = "index"
	}
	read := func() (git.Snapshot, error) {
		if head != "" {
			return repo.Base(ctx, head)
		}
		if req.Staged {
			return repo.Staged(ctx)
		}
		snapshot, _, err := repo.Working(ctx)
		return snapshot, err
	}
	observed, err := read()
	if err != nil {
		return nil, err
	}
	origin, err := repo.Origin(ctx)
	if err != nil {
		return nil, err
	}
	prepared, err := prepareCaptured(ctx, observed, repo.Root, origin, input, head)
	if err != nil {
		return nil, err
	}
	result := &prepared.Report
	// Mutable inputs are observed twice, like diff. This detects intervening edits
	// but cannot turn filesystem reads into an atomic snapshot.
	if head == "" {
		latest, err := read()
		if err != nil {
			return nil, err
		}
		if latest.Digest() != result.Snapshot {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "snapshot_changed", Message: "repository contents changed during snapshot capture"})
		}
	}
	result.Complete = len(result.Diagnostics) == 0
	return prepared, nil
}

// prepareCaptured also prepares diff's commit/index/patch versions without an
// extra filesystem capture that could change their identity or ownership.
func prepareCaptured(ctx context.Context, contents git.Snapshot, checkout, origin, input, head string) (*PreparedSnapshot, error) {
	manifests, err := codegraph.BuildManifests(ctx, contents.Files)
	if err != nil {
		return nil, fmt.Errorf("prepare %s manifests: %w", input, err)
	}
	layout, err := project.Load(contents.Files, origin, manifests)
	if err != nil {
		return nil, fmt.Errorf("prepare %s repository context: %w", input, err)
	}
	report := SnapshotReport{
		Layout: layout, SchemaVersion: 1, Checkout: checkout, Input: input, Head: head,
		Snapshot: contents.Digest(), FileCount: len(contents.Files) + len(contents.Opaque),
		Diagnostics:  []Diagnostic{},
		Observations: manifests.Report().Diagnostics,
	}
	for _, issue := range contents.Issues {
		report.Diagnostics = append(report.Diagnostics, diagnostic("snapshot_incomplete", issue))
	}
	report.Complete = len(report.Diagnostics) == 0
	return &PreparedSnapshot{Report: report, Contents: contents, Manifests: manifests}, nil
}
