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
	"github.com/compforge/repocli"
)

type commandLogKey struct{}

// diffRecord stores replay inputs and a native timeline, without duplicating the analysis report.
// Commit IDs can be replayed; mutable inputs still require the original bytes.
type diffRecord struct {
	SchemaVersion int               `json:"schemaVersion"`
	Time          time.Time         `json:"time"`
	RunID         string            `json:"runId"`
	Version       string            `json:"version"`
	Checkout      string            `json:"checkout"`
	From          string            `json:"from"`
	To            string            `json:"to"`
	Input         string            `json:"input"`
	Snapshot      string            `json:"snapshot"`
	TestDirs      []string          `json:"testDirs"`
	ChangedFiles  []string          `json:"changedFiles"`
	PatchFile     string            `json:"patchFile,omitempty"`
	Timeout       string            `json:"timeout"`
	Status        string            `json:"status"`
	Timeline      timeline.Snapshot `json:"timeline"`
}

func recordDiff(ctx context.Context, request repocli.DiffRequest, result repocli.DiffReport, timeout time.Duration, operation timeline.Timeline, analysisErr error) {
	run, _ := ctx.Value(commandLogKey{}).(*commandLog)
	if run == nil || run.file == nil {
		return // Shared logging setup already reports unavailable storage.
	}
	// This recorder uses only private memory. Collect after cancellation so the
	// failed stage remains observable without changing the analysis result.
	snapshot, collectionErr := operation.Finish(context.WithoutCancel(ctx), analysisErr)
	if collectionErr != nil {
		run.writer.warn(collectionErr)
	}
	to := result.Head
	if to == "" {
		to = result.Input
	}
	record := diffRecord{
		SchemaVersion: 5, Time: time.Now(), RunID: run.runID, Version: Version,
		Checkout: result.Checkout, From: result.Base, To: to, Input: result.Input,
		Snapshot:     result.Snapshot,
		TestDirs:     append([]string{}, request.TestDirs...),
		ChangedFiles: append([]string{}, request.ChangedFiles...), PatchFile: request.PatchFile,
		Timeout: timeout.String(), Status: diffAnalysisStatus(analysisErr), Timeline: snapshot,
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

func startDiffTimeline(ctx context.Context) timeline.Timeline {
	run, _ := ctx.Value(commandLogKey{}).(*commandLog)
	if run == nil || run.file == nil {
		return timeline.Noop("")
	}
	operation, err := timeline.New(run.runID)
	if err == nil {
		// Even an already-expired invocation records its analysis attempt.
		err = operation.Start(context.WithoutCancel(ctx), "diff.analysis")
	}
	if err != nil {
		run.writer.warn(err)
		return timeline.Noop(run.runID)
	}
	return operation
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
