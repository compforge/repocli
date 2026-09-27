package analysis

import (
	"context"

	"github.com/compforge/repocli/internal/project"
)

// SnapshotRequest selects one content source, without a comparison or impact scan.
type SnapshotRequest struct {
	Repository string
	Head       string
	Staged     bool
}

// SnapshotReport uses its own schema; Snapshot shares the diff digest contract.
type SnapshotReport struct {
	project.Layout
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
	prepared, err := Prepare(ctx, req)
	if err != nil {
		return SnapshotReport{}, err
	}
	return prepared.Report, nil
}
