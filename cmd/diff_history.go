package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/compforge/go-stdx/timeline"
	"github.com/compforge/repocli/internal/analysis"
)

type commandLogKey struct{}

type diffTimelineStep struct {
	Name       string         `json:"name"`
	AtMS       int64          `json:"atMs"`
	DurationMS int64          `json:"durationMs"`
	Fields     map[string]any `json:"fields,omitempty"`
}

type diffTimeline struct {
	TotalMS int64              `json:"totalMs"`
	Steps   []diffTimelineStep `json:"steps"`
}

// diffRecord stores comparison identity and query inputs, never repository contents.
// Commit IDs can be replayed; mutable inputs still require the original bytes.
type diffRecord struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Time          time.Time             `json:"time"`
	RunID         string                `json:"runId"`
	Version       string                `json:"version"`
	Checkout      string                `json:"checkout"`
	From          string                `json:"from"`
	To            string                `json:"to"`
	Input         string                `json:"input"`
	Snapshot      string                `json:"snapshot"`
	ImpactMode    string                `json:"impactMode"`
	TestDirs      []string              `json:"testDirs"`
	ChangedFiles  []string              `json:"changedFiles"`
	PatchFile     string                `json:"patchFile,omitempty"`
	Timeout       string                `json:"timeout"`
	Status        string                `json:"status"`
	Timeline      diffTimeline          `json:"timeline"`
	Scope         string                `json:"scope"`
	Complete      bool                  `json:"complete"`
	Diagnostics   []analysis.Diagnostic `json:"diagnostics"`
	TestFiles     []string              `json:"testFiles"`
}

func recordDiff(ctx context.Context, request analysis.Request, result analysis.Report, timeout time.Duration, snapshot timeline.Snapshot, analysisErr error) {
	run, _ := ctx.Value(commandLogKey{}).(*commandLog)
	if run == nil || run.file == nil {
		return // Shared logging setup already reports unavailable storage.
	}
	to := result.Head
	if to == "" {
		to = result.Input
	}
	record := diffRecord{
		SchemaVersion: 2, Time: time.Now(), RunID: run.runID, Version: Version,
		Checkout: result.Checkout, From: result.Base, To: to, Input: result.Input,
		Snapshot: result.Snapshot, ImpactMode: result.ImpactMode,
		TestDirs:     append([]string{}, request.TestDirs...),
		ChangedFiles: append([]string{}, request.ChangedFiles...), PatchFile: request.PatchFile,
		Timeout: timeout.String(), Status: diffAnalysisStatus(analysisErr), Timeline: diffTimelineFrom(snapshot),
		Scope: result.Scope, Complete: result.Complete,
		Diagnostics: result.Diagnostics, TestFiles: append([]string{}, result.TestFiles...),
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

func diffTimelineFrom(snapshot timeline.Snapshot) diffTimeline {
	steps := make([]diffTimelineStep, 0, len(snapshot.Steps))
	for _, step := range snapshot.Steps {
		fields := map[string]any{}
		for _, field := range step.Fields {
			fields[field.Key] = field.Value
		}
		steps = append(steps, diffTimelineStep{
			Name: step.Message, AtMS: step.Time.Sub(snapshot.StartTime).Milliseconds(),
			DurationMS: step.Duration.Milliseconds(), Fields: fields,
		})
	}
	return diffTimeline{TotalMS: snapshot.Duration().Milliseconds(), Steps: steps}
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
