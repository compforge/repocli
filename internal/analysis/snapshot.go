package analysis

import (
	"context"
	"fmt"

	"github.com/compforge/repocli/internal/git"
)

// SnapshotRequest selects one content source, without a comparison or impact scan.
type SnapshotRequest struct {
	Repository string
	Head       string
	Staged     bool
}

// SnapshotReport uses its own schema; Snapshot shares the diff digest contract.
type SnapshotReport struct {
	SchemaVersion int          `json:"schemaVersion"`
	Checkout      string       `json:"checkout"`
	Input         string       `json:"input"`
	Head          string       `json:"head,omitempty"`
	Snapshot      string       `json:"snapshot"`
	FileCount     int          `json:"fileCount"`
	Complete      bool         `json:"complete"`
	Diagnostics   []Diagnostic `json:"diagnostics"`
}

func CaptureSnapshot(ctx context.Context, req SnapshotRequest) (SnapshotReport, error) {
	if req.Head != "" && req.Staged {
		return SnapshotReport{}, fmt.Errorf("head and staged are mutually exclusive")
	}
	repo, err := git.Open(ctx, req.Repository)
	if err != nil {
		return SnapshotReport{}, err
	}
	result := SnapshotReport{SchemaVersion: 1, Checkout: repo.Root, Input: "working_tree", Diagnostics: []Diagnostic{}}
	if req.Head != "" {
		result.Input = "commit"
		result.Head, err = repo.Resolve(ctx, req.Head)
		if err != nil {
			return SnapshotReport{}, err
		}
	} else if req.Staged {
		result.Input = "index"
	}
	read := func() (git.Snapshot, error) {
		if result.Head != "" {
			return repo.Base(ctx, result.Head)
		}
		if req.Staged {
			return repo.Staged(ctx)
		}
		snapshot, _, err := repo.Working(ctx)
		return snapshot, err
	}
	observed, err := read()
	if err != nil {
		return SnapshotReport{}, err
	}
	result.Snapshot = observed.Digest()
	result.FileCount = len(observed.Files)
	for _, issue := range observed.Issues {
		result.Diagnostics = append(result.Diagnostics, diagnostic("snapshot_incomplete", issue))
	}
	// Mutable inputs are observed twice, like diff. This detects intervening edits
	// but cannot turn filesystem reads into an atomic snapshot.
	if result.Head == "" {
		latest, err := read()
		if err != nil {
			return SnapshotReport{}, err
		}
		if latest.Digest() != result.Snapshot {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "snapshot_changed", Message: "repository contents changed during snapshot capture"})
		}
	}
	result.Complete = len(result.Diagnostics) == 0
	return result, nil
}
