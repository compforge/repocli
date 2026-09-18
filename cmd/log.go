package cmd

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/compforge/repocli/internal/analysis"
)

type diffLog struct {
	logger  *slog.Logger
	file    *os.File
	writer  *logWriter
	started time.Time
}

// slog ignores handler write errors. Surface one warning while preserving the
// analysis result, including when an already-open log stops accepting writes.
type logWriter struct {
	output io.Writer
	stderr io.Writer
	once   sync.Once
}

func (w *logWriter) warn(err error) {
	w.once.Do(func() { fmt.Fprintln(w.stderr, "repocli: log warning:", err) })
}

func (w *logWriter) Write(data []byte) (int, error) {
	record := append([]byte("[repocli] "), data...)
	// One append write per record keeps concurrent CLI processes from overwriting
	// each other; there is no rename/truncate rotation of the active daily file.
	n, err := w.output.Write(record)
	if err == nil && n != len(record) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.warn(err)
		return 0, err
	}
	return len(data), nil
}

func startDiffLog(disabled bool, stderr io.Writer, request analysis.Request, timeout time.Duration) *diffLog {
	now := time.Now()
	run := &diffLog{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), started: now}
	if disabled {
		return run
	}
	writer := &logWriter{stderr: stderr}
	home, err := os.UserHomeDir()
	if err != nil {
		writer.warn(err)
		return run
	}
	dir := filepath.Join(home, ".repocli", "logs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		writer.warn(err)
		return run
	}
	file, err := os.OpenFile(filepath.Join(dir, now.Format("2006-01-02")+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		writer.warn(err)
		return run
	}
	writer.output = file
	run.file, run.writer = file, writer
	run.logger = slog.New(slog.NewTextHandler(writer, nil)).With("run_id", rand.Text(), "version", Version)
	if err := pruneLogs(dir, now); err != nil {
		writer.warn(err)
	}
	requestedRepo, err := filepath.Abs(request.Repository)
	if err != nil {
		requestedRepo = request.Repository
	}
	input := "working_tree"
	switch {
	case request.PatchFile != "":
		input = "patch"
	case request.Staged:
		input = "index"
	case request.Head != "":
		input = "commit"
	}
	run.logger.Info("diff.started", "requested_repo", requestedRepo, "input", input,
		"base", request.Base, "head", request.Head, "impact", request.Mode,
		"test_dirs", request.TestDirs, "changed_file_filters", len(request.ChangedFiles), "timeout", timeout.String())
	return run
}

// Keep today and the preceding 29 local calendar dates. Only our dated regular
// files are eligible: unrelated files, directories and symlinks are left alone.
func pruneLogs(dir string, now time.Time) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	cutoff := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -29)
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		date, err := time.ParseInLocation("2006-01-02", strings.TrimSuffix(entry.Name(), ".log"), now.Location())
		if err != nil || !date.Before(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (run *diffLog) finish(ctx context.Context, result analysis.Report, err error) {
	if run.file == nil {
		return
	}
	defer func() {
		if err := run.file.Close(); err != nil {
			run.writer.warn(err)
		}
	}()
	for _, diagnostic := range result.Diagnostics {
		run.logger.Warn("diff.diagnostic", "code", diagnostic.Code, "path", diagnostic.Path, "detail", diagnostic.Message)
	}
	if err != nil {
		stage := "analysis"
		if result.SchemaVersion != 0 {
			stage = "output"
		}
		code := "execution_failed"
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			code = "timeout"
		case errors.Is(ctx.Err(), context.Canceled):
			code = "canceled"
		}
		fields := []any{"stage", stage, "code", code, "elapsed_ms", time.Since(run.started).Milliseconds(), "exit_code", 1}
		// Parser/process errors may quote patch lines or subprocess output. Keep the
		// original error on stderr, rather than persist source or credentials to disk.
		run.logger.Error("diff.failed", fields...)
		return
	}
	run.logger.Info("diff.finished", "checkout", result.Checkout, "base", result.Base, "head", result.Head,
		"snapshot", result.Snapshot, "complete", result.Complete, "scope", result.Scope,
		"changed_files", len(result.Changes), "source_files", len(result.SourceFiles), "test_files", len(result.TestFiles),
		"components", len(result.Components), "diagnostics", len(result.Diagnostics),
		"elapsed_ms", time.Since(run.started).Milliseconds(), "exit_code", 0)
}
