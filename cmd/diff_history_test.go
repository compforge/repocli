package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func historyPath(home string) string {
	return filepath.Join(home, ".repocli", "logs", "diff-"+time.Now().Format("2006-01-02")+".jsonl")
}

func readDiffHistory(t *testing.T, home string) []diffRecord {
	t.Helper()
	data, err := os.ReadFile(historyPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		t.Fatal("unterminated record")
	}
	var records []diffRecord
	for _, line := range bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n")) {
		var record diffRecord
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("corrupt history: %v", err)
		}
		records = append(records, record)
	}
	return records
}

func TestDiffHistoryCapturesComparisonAndAppends(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := fixture(t)
	base := strings.TrimSpace(gitCommand(t, repo, "rev-parse", "HEAD"))
	put(t, repo, "source file.ts", "export function a() { return 9; }\nexport function b() { return 2; }\n")
	args := []string{"diff", "--repo", repo, "--impact", "file", "--test-dir", "tests", "--changed-file", "source file.ts", "--timeout", "15s"}
	report := runJSON(t, append(args, "--json"), "")
	var out, stderr bytes.Buffer
	if code := Execute(context.Background(), args, nil, &out, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("text diff: %d %s", code, stderr.String())
	}
	records := readDiffHistory(t, home)
	if len(records) != 2 {
		t.Fatalf("records: %d", len(records))
	}
	for _, record := range records {
		if record.SchemaVersion != 2 || record.Status != "completed" || record.Version != Version || record.From != base || record.To != "working_tree" || record.Input != report.Input || record.Checkout != report.Checkout || record.Snapshot != report.Snapshot {
			t.Fatalf("identity: %+v", record)
		}
		if record.ImpactMode != "file" || record.Timeout != "15s" || !reflect.DeepEqual(record.TestDirs, []string{"tests"}) || !reflect.DeepEqual(record.ChangedFiles, []string{"source file.ts"}) || !reflect.DeepEqual(record.TestFiles, report.TestFiles) {
			t.Fatalf("query: %+v", record)
		}
		if record.Time.IsZero() || !strings.Contains(readLogs(t, home), "run_id="+record.RunID) {
			t.Fatal("missing execution link")
		}
		if len(record.Timeline.Steps) < 4 || record.Timeline.Steps[0].Name != "analysis.snapshot" {
			t.Fatalf("missing stage timings: %+v", record.Timeline)
		}
		for _, step := range record.Timeline.Steps {
			if step.AtMS < 0 || step.DurationMS < 0 || step.AtMS > record.Timeline.TotalMS {
				t.Fatalf("invalid stage timing: %+v", record.Timeline)
			}
		}
	}
	if records[0].RunID == records[1].RunID {
		t.Fatal("reused run ID")
	}
	info, err := os.Stat(historyPath(home))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("permissions: %v %v", info, err)
	}
}

func TestDiffHistoryCommitIndexPatchAndPartial(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := fixture(t)
	base := strings.TrimSpace(gitCommand(t, repo, "rev-parse", "HEAD"))
	put(t, repo, "source file.ts", "export function a() { return 9; }\nexport function b() { return 2; }\n")
	patch := gitCommand(t, repo, "diff", "--binary", "HEAD")
	gitCommand(t, repo, "add", "source file.ts")
	for _, flags := range [][]string{{"--staged"}, {"--file", "-"}} {
		report := runJSON(t, append([]string{"diff", "--repo", repo, "--json"}, flags...), patch)
		records := readDiffHistory(t, home)
		last := records[len(records)-1]
		if last.To != report.Input || last.Snapshot != report.Snapshot || last.TestFiles == nil || len(last.TestFiles) != 0 {
			t.Fatalf("mutable input: %+v", last)
		}
		if report.Input == "patch" && last.PatchFile != "-" {
			t.Fatal("lost stdin patch mode")
		}
	}
	gitCommand(t, repo, "commit", "-qm", "change")
	head := strings.TrimSpace(gitCommand(t, repo, "rev-parse", "HEAD"))
	runJSON(t, []string{"diff", "--repo", repo, "--base", base, "--head", "HEAD", "--json"}, "")
	records := readDiffHistory(t, home)
	if records[2].From != base || records[2].To != head || records[2].Input != "commit" {
		t.Fatal(records[2])
	}
	put(t, repo, "resource.md", "changed resource\n")
	report := runJSON(t, []string{"diff", "--repo", repo, "--test-dir", "tests", "--json"}, "")
	records = readDiffHistory(t, home)
	last := records[len(records)-1]
	if report.Complete || last.Complete || last.Scope != report.Scope || !reflect.DeepEqual(last.Diagnostics, report.Diagnostics) {
		t.Fatalf("partial result lost: %+v", last)
	}
}

func TestDiffHistoryFailureDoesNotChangeResult(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(historyPath(home), 0700); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"diff", "--repo", fixture(t), "--json"}, nil, &out, &stderr)
	if code != 0 || !json.Valid(out.Bytes()) || strings.Count(stderr.String(), "repocli: log warning:") != 1 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
}

func TestDiffHistoryRecordsAnalysisFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := fixture(t)
	for _, args := range [][]string{{"version"}, {"snapshot", "--repo", repo}, {"diff", "--help"}, {"diff", "--impact", "invalid"}} {
		var out, stderr bytes.Buffer
		Execute(context.Background(), args, nil, &out, &stderr)
	}
	if _, err := os.Stat(historyPath(home)); !os.IsNotExist(err) {
		t.Fatalf("unexpected history before analysis: %v", err)
	}
	var failedOut, failedErr bytes.Buffer
	if code := Execute(context.Background(), []string{"diff", "--repo", repo, "--base", "absent-ref"}, nil, &failedOut, &failedErr); code == 0 {
		t.Fatal("missing base unexpectedly succeeded")
	}
	failed := readDiffHistory(t, home)
	if len(failed) != 1 || failed[0].Status != "failed" || len(failed[0].Timeline.Steps) != 1 || failed[0].Timeline.Steps[0].Name != "analysis.snapshot" || failed[0].Complete {
		t.Fatalf("failed analysis history: %+v", failed)
	}
	var stderr bytes.Buffer
	if code := Execute(context.Background(), []string{"diff", "--repo", repo, "--json"}, nil, failedWriter{}, &stderr); code != 1 {
		t.Fatalf("exit=%d", code)
	}
	records := readDiffHistory(t, home)
	if len(records) != 2 || records[1].Status != "completed" {
		t.Fatal("completed analysis lost on stdout failure")
	}
}

func TestDiffAnalysisStatus(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, "completed"},
		{context.DeadlineExceeded, "deadline_exceeded"},
		{context.Canceled, "canceled"},
		{os.ErrNotExist, "failed"},
	} {
		if got := diffAnalysisStatus(tc.err); got != tc.want {
			t.Fatalf("status(%v) = %s, want %s", tc.err, got, tc.want)
		}
	}
}

func TestDiffHistoryDeadlineIncludesTimeline(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := fixture(t)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	var out, stderr bytes.Buffer
	if code := Execute(ctx, []string{"diff", "--repo", repo}, nil, &out, &stderr); code == 0 {
		t.Fatal("expired context unexpectedly succeeded")
	}
	records := readDiffHistory(t, home)
	if len(records) != 1 || records[0].Status != "deadline_exceeded" || len(records[0].Timeline.Steps) != 1 || records[0].Timeline.Steps[0].Name != "analysis.snapshot" {
		t.Fatalf("deadline history: %+v", records)
	}
}

func TestConcurrentDiffHistory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := fixture(t)
	const count = 6
	var wg sync.WaitGroup
	for range count {
		wg.Go(func() {
			var out, stderr bytes.Buffer
			if code := Execute(context.Background(), []string{"diff", "--repo", repo, "--json"}, nil, &out, &stderr); code != 0 || stderr.Len() != 0 {
				t.Errorf("exit=%d stderr=%s", code, stderr.String())
			}
		})
	}
	wg.Wait()
	records := readDiffHistory(t, home)
	if len(records) != count {
		t.Fatalf("records=%d", len(records))
	}
	seen := map[string]bool{}
	for _, record := range records {
		if seen[record.RunID] {
			t.Fatal("duplicate run")
		}
		seen[record.RunID] = true
	}
}

func TestDiffHistoryRetention(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 3, 15, 12, 0, 0, 0, time.Local)
	expired := "diff-" + now.AddDate(0, 0, -30).Format("2006-01-02") + ".jsonl"
	kept := []string{"diff-" + now.AddDate(0, 0, -29).Format("2006-01-02") + ".jsonl", "diff-2026-03-15.jsonl", "other-2020-01-01.jsonl", "diff-invalid.jsonl", "diff-2020-01-01.jsonl.bak"}
	for _, name := range append(kept, expired) {
		put(t, dir, name, "record")
	}
	if err := os.Mkdir(filepath.Join(dir, "diff-2020-01-01.jsonl"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, kept[0]), filepath.Join(dir, "diff-2020-01-02.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := pruneLogs(dir, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, expired)); !os.IsNotExist(err) {
		t.Fatalf("expired: %v", err)
	}
	for _, name := range append(kept, "diff-2020-01-01.jsonl", "diff-2020-01-02.jsonl") {
		if _, err := os.Lstat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("removed %s: %v", name, err)
		}
	}
}
