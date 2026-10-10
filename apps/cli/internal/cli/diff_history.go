package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/compforge/go-stdx/timeline"
	"github.com/compforge/repocli/toolkit/go"
)

type commandLogKey struct{}

// diffRecord stores replay inputs and a native timeline, without duplicating the analysis report.
// Commit IDs can be replayed; mutable inputs still require the original bytes.
type diffRecord struct {
	SchemaVersion    int               `json:"schemaVersion"`
	Time             time.Time         `json:"time"`
	RunID            string            `json:"runId"`
	Version          string            `json:"version"`
	Checkout         string            `json:"checkout"`
	From             string            `json:"from"`
	To               string            `json:"to"`
	Input            string            `json:"input"`
	Snapshot         string            `json:"snapshot"`
	TestDirs         []string          `json:"testDirs"`
	ChangedFiles     []string          `json:"changedFiles"`
	PatchFile        string            `json:"patchFile,omitempty"`
	Timeout          string            `json:"timeout"`
	MaxFiles         int               `json:"maxFiles"`
	MaxSnapshotBytes int64             `json:"maxSnapshotBytes"`
	MaxNodes         int               `json:"maxNodes"`
	MaxRelations     int               `json:"maxRelations"`
	Status           string            `json:"status"`
	Timeline         timeline.Snapshot `json:"timeline"`
}

func recordDiff(ctx context.Context, request repocli.DiffRequest, record diffRecord, timeout time.Duration, snapshot timeline.Snapshot, analysisErr error) {
	run, _ := ctx.Value(commandLogKey{}).(*commandLog)
	if run == nil || run.file == nil {
		return // Shared logging setup already reports unavailable storage.
	}
	to := record.To
	if to == "" {
		to = record.Input
	}
	record = diffRecord{
		SchemaVersion: 5, Time: time.Now(), RunID: run.runID, Version: Version,
		Checkout: record.Checkout, From: record.From, To: to, Input: record.Input,
		Snapshot:     record.Snapshot,
		TestDirs:     append([]string{}, request.TestDirs...),
		ChangedFiles: append([]string{}, request.ChangedFiles...), PatchFile: request.PatchFile,
		Timeout: timeout.String(), Status: diffAnalysisStatus(analysisErr), Timeline: snapshot,
		MaxFiles: request.MaxFiles, MaxNodes: request.MaxNodes, MaxRelations: request.MaxRelations, MaxSnapshotBytes: request.MaxSnapshotBytes,
	}
	path := filepath.Join(filepath.Dir(run.file.Name()), "diff-"+run.started.Format("2006-01-02")+".jsonl")
	if err := appendDiffRecord(path, record); err != nil {
		run.writer.warn(err)
	}
}

func diffAnalysisStatus(err error) string {
	switch {
	case err == nil:
		return "completed"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "failed"
	}
}

// The command owns the operation; export availability does not own its lifetime.
func startDiffTimeline(ctx context.Context, operation string) string {
	run, _ := ctx.Value(commandLogKey{}).(*commandLog)
	if run == nil {
		return ""
	}
	if err := timeline.Start(run.runID, operation); err != nil {
		run.logger.Warn("timeline start failed", "error", err)
		return ""
	}
	return run.runID
}

func finishDiffTimeline(ctx context.Context, id string, result error) timeline.Snapshot {
	if id == "" {
		return timeline.Snapshot{}
	}
	run, _ := ctx.Value(commandLogKey{}).(*commandLog)
	if err := timeline.Finish(id, result); err != nil {
		run.logger.Warn("timeline finish failed", "error", err)
	}
	// Collect cancellation facts after the command deadline without changing its result.
	snapshot, err := timeline.Read(context.WithoutCancel(ctx), id, false)
	if err != nil {
		run.logger.Warn("timeline read failed", "error", err)
	}
	return snapshot
}

func appendDiffRecord(path string, record diffRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	// One append write per complete JSON line avoids interleaving concurrent CLI runs.
	data = append(data, '\n')
	n, err := file.Write(data)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return closeErr
}
