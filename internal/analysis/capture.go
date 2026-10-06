package analysis

import (
	"context"

	"github.com/compforge/repocli/internal/git"
)

// CapturedSnapshot is command-scoped: consumers treat both its metadata and bytes
// as read-only. Refresh replaces the whole value, never just the source contents.
type CapturedSnapshot struct {
	Report   SnapshotReport
	Contents git.Snapshot
}

// Capture reads content for snapshot identity and commands that need source bytes.
// +spec=`Content capture is independent of repository configuration validity`
// +why=`Selected Git contents, rather than project configuration validity, define snapshot identity`
func Capture(ctx context.Context, req InputRequest) (*CapturedSnapshot, error) {
	repo, input, head, err := selectInput(ctx, req)
	if err != nil {
		return nil, err
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
	captured := capturedReport(observed, repo.Root, input, head)
	result := &captured.Report
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
	return captured, nil
}

// capturedReport describes content identity independently of project configuration.
func capturedReport(contents git.Snapshot, checkout, input, head string) *CapturedSnapshot {
	report := SnapshotReport{
		SchemaVersion: 2, Checkout: checkout, Input: input, Head: head,
		Snapshot: contents.Digest(), FileCount: len(contents.Files) + len(contents.Opaque),
		Diagnostics: []Diagnostic{},
	}
	for _, issue := range contents.Issues {
		report.Diagnostics = append(report.Diagnostics, diagnostic("snapshot_incomplete", issue))
	}
	report.Complete = len(report.Diagnostics) == 0
	return &CapturedSnapshot{Report: report, Contents: contents}
}
