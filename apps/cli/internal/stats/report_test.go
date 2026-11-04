package stats

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func put(t *testing.T, root, name, data string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

// +case:id=stats_command_join,desc=`Interleaved commands include quoted fields, an orphan completion and an unfinished start`,expect=`Count outcomes once, retain missing completions separately, and calculate exact nearest-rank percentiles`
func TestCommandOutcomesAndPercentiles(t *testing.T) {
	dir := t.TempDir()
	lines := []string{
		`[repocli] time=2026-10-01T10:00:00Z level=INFO msg=command.started run_id=a cwd="/tmp/a b" args="[\"diff\",\"a b\"]"`,
		`[repocli] time=2026-10-01T10:00:00Z level=INFO msg=command.started run_id=b cwd=/tmp/b`,
		`[repocli] time=2026-10-01T10:00:00Z level=INFO msg=command.started run_id=open cwd=/tmp/c`,
		`[repocli] time=2026-10-01T10:00:01Z level=INFO msg=command.finished run_id=b command="repocli diff" elapsed_ms=1000 exit_code=0`,
		`[repocli] time=2026-10-01T10:00:02Z level=ERROR msg=command.failed run_id=a command="repocli diff" elapsed_ms=2000 exit_code=1`,
		`[repocli] time=2026-10-01T10:00:03Z level=INFO msg=command.finished run_id=orphan command="repocli diff" elapsed_ms=3000 exit_code=0`,
	}
	put(t, dir, "logs/2026-10-01.log", strings.Join(lines, "\n")+"\n")
	put(t, dir, "logs/unrelated.log", "not a repocli log")
	r, err := Read(context.Background(), dir, 2)
	if err != nil || !r.Complete {
		t.Fatalf("%+v %v", r, err)
	}
	if len(r.Commands.ByCommand) != 1 || r.Commands.Unfinished != 1 {
		t.Fatalf("%+v", r.Commands)
	}
	c := r.Commands.ByCommand[0]
	if c.Succeeded != 2 || c.Failed != 1 || c.Duration.TotalSeconds != 6 || c.Duration.P50Seconds != 2 || c.Duration.P95Seconds != 3 {
		t.Fatalf("%+v", c)
	}
	if len(r.Commands.Slowest) != 2 || r.Commands.Slowest[0].RunID != "orphan" || r.Commands.Slowest[1].CWD != "/tmp/a b" {
		t.Fatalf("%+v", r.Commands.Slowest)
	}
	// Reading old dated logs must not trigger the normal CLI retention sweep.
	if _, err := os.Stat(filepath.Join(dir, "logs/2026-10-01.log")); err != nil {
		t.Fatal(err)
	}
}

// +case:id=stats_partial_records,desc=`Diff logs contain a valid nested timeline followed by unsupported and truncated records`,expect=`Keep valid statistics, report gaps, and never sum nested stages as operation time`
func TestAnalysisGapsAndNestedTimings(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "logs/diff-2026-10-01.jsonl", `{"schemaVersion":5,"time":"2026-10-01T10:00:10Z","runId":"a","status":"completed","timeline":{"id":"a","started_at":"2026-10-01T10:00:00Z","finished_at":"2026-10-01T10:00:10Z","stages":[{"id":"p","name":"parent","started_at":"2026-10-01T10:00:00Z","finished_at":"2026-10-01T10:00:10Z"},{"id":"c","parent_id":"p","name":"child","started_at":"2026-10-01T10:00:01Z","finished_at":"2026-10-01T10:00:06Z"}]}}
{"schemaVersion":5,"time":"2026-10-01T10:00:11Z","runId":"b","status":"failed"}
{"schemaVersion":99}
{"schemaVersion":
`)
	r, err := Read(context.Background(), dir, 5)
	if err != nil || r.Complete || len(r.Diagnostics) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	if r.Analysis.Statuses["completed"] != 1 || r.Analysis.Statuses["failed"] != 1 || r.Analysis.Untimed != 1 || r.Analysis.Duration.TotalSeconds != 10 {
		t.Fatalf("%+v", r.Analysis)
	}
	if len(r.Analysis.Stages) != 2 || r.Analysis.Stages[0].Duration.TotalSeconds != 5 || r.Analysis.Stages[1].Duration.TotalSeconds != 10 {
		t.Fatalf("%+v", r.Analysis.Stages)
	}
	if !strings.Contains(r.Diagnostics[0], "2 invalid records") {
		t.Fatal(r.Diagnostics)
	}
}

func TestStorageDoesNotFollowSymlinksAndMissingRootIsEmpty(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	put(t, dir, "cache/a", "12345")
	put(t, outside, "external", "never counted")
	if err := os.Symlink(outside, filepath.Join(dir, "cache/link")); err != nil {
		t.Fatal(err)
	}
	r, err := Read(context.Background(), dir, 5)
	if err != nil || !r.Complete || len(r.Storage) != 1 || r.Storage[0].Bytes != 5 || r.Storage[0].Files != 1 || r.Storage[0].Symlinks != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	missing := filepath.Join(dir, "missing")
	r, err = Read(context.Background(), missing, 5)
	if err != nil || !r.Complete || len(r.Storage) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created directory: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Read(ctx, dir, 5); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestMalformedCommandDoesNotCountAsSuccess(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "logs/2026-10-01.log", `[repocli] time=2026-10-01T10:00:01Z msg=command.finished run_id=a command="repocli diff" elapsed_ms=invalid exit_code=0
`)
	r, err := Read(context.Background(), dir, 5)
	if err != nil || r.Complete || len(r.Commands.ByCommand) != 0 || len(r.Diagnostics) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
}
