package analysis

import "context"

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

func CaptureSnapshot(ctx context.Context, req InputRequest) (SnapshotReport, error) {
	captured, err := Capture(ctx, req)
	if err != nil {
		return SnapshotReport{}, err
	}
	return captured.Report, nil
}
