package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

func readLogs(t *testing.T, home string) string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(home, ".repocli", "logs", "*.log"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("log files: %v, %v", paths, err)
	}
	var all strings.Builder
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		all.Write(data)
	}
	return all.String()
}

func TestDiffLogsDoNotChangeReport(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := fixture(t)
	put(t, dir, "source file.ts", "export function a() { return 7; }\nexport function b() { return 2; }\n")
	args := []string{"diff", "--repo", dir, "--test-dir", "tests", "--json"}
	logged := runJSON(t, args, "")
	silent := runJSON(t, append(args, "--no-log"), "")
	if !reflect.DeepEqual(logged, silent) {
		t.Fatal("logging changed report")
	}
	logs := readLogs(t, home)
	for _, want := range []string{"[repocli] time=", "msg=diff.started", "version=" + Version, "input=working_tree", "msg=diff.finished", "scope=focused", "source_files=1", "test_files=1", "exit_code=0"} {
		if !strings.Contains(logs, want) {
			t.Errorf("missing %q in %s", want, logs)
		}
	}
	ids := regexp.MustCompile(`run_id=([^ ]+)`).FindAllStringSubmatch(logs, -1)
	if len(ids) != 2 || ids[0][1] != ids[1][1] {
		t.Fatalf("run IDs: %v", ids)
	}
	if strings.Contains(logs, "export function") {
		t.Fatal("source contents in logs")
	}
	for path, perm := range map[string]os.FileMode{
		filepath.Join(home, ".repocli", "logs"):                                         0700,
		filepath.Join(home, ".repocli", "logs", time.Now().Format("2006-01-02")+".log"): 0600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != perm {
			t.Errorf("%s mode %v", path, info.Mode())
		}
	}
}

func TestDiffLogsDiagnostics(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := fixture(t)
	put(t, dir, "Makefile", "all:\n\t@true\n")
	result := runJSON(t, []string{"diff", "--repo", dir, "--test-dir", "tests", "--json"}, "")
	if result.Complete || len(result.Diagnostics) == 0 {
		t.Fatalf("expected diagnostic: %+v", result)
	}
	logs := readLogs(t, home)
	if !strings.Contains(logs, "msg=diff.diagnostic") || !strings.Contains(logs, "complete=false") {
		t.Fatal(logs)
	}
	for _, d := range result.Diagnostics {
		if !strings.Contains(logs, "code="+d.Code) {
			t.Fatalf("missing diagnostic %s: %s", d.Code, logs)
		}
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, fmt.Errorf("private-output-error") }

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestDiffLogsFailures(t *testing.T) {
	for _, kind := range []string{"analysis", "output", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			repo := t.TempDir()
			if kind != "analysis" {
				repo = fixture(t)
			}
			var out, stderr bytes.Buffer
			var output io.Writer = &out
			if kind == "output" {
				output = failingWriter{}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if kind == "canceled" {
				cancel()
			}
			code := Execute(ctx, []string{"diff", "--repo", repo, "--json"}, strings.NewReader(""), output, &stderr)
			if code != 1 || stderr.Len() == 0 {
				t.Fatalf("code %d: %s", code, stderr.String())
			}
			logs := readLogs(t, home)
			stage := kind
			if kind == "canceled" {
				stage = "analysis"
			}
			for _, want := range []string{"msg=diff.failed", "stage=" + stage, "exit_code=1"} {
				if !strings.Contains(logs, want) {
					t.Errorf("missing %s: %s", want, logs)
				}
			}
			if kind == "canceled" && !strings.Contains(logs, "code=canceled") {
				t.Fatal(logs)
			}
			if strings.Contains(logs, "private-output-error") || strings.Contains(logs, "msg=diff.finished") {
				t.Fatal(logs)
			}
		})
	}
}

func TestCommandsThatDoNotLog(t *testing.T) {
	dir := fixture(t)
	for _, args := range [][]string{
		{"--help"}, {"diff", "--help"}, {"version"}, {"--version"},
		{"diff", "--timeout", "0s"}, {"diff", "--head", "HEAD", "--staged"},
		{"diff", "--repo", dir, "--no-log"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			Execute(context.Background(), args, strings.NewReader(""), io.Discard, io.Discard)
			if _, err := os.Stat(filepath.Join(home, ".repocli")); !os.IsNotExist(err) {
				t.Fatalf("unexpected logging: %v", err)
			}
		})
	}
}

func TestLogFailurePreservesSuccessfulDiff(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	put(t, home, ".repocli", "block log directory")
	var out, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"diff", "--repo", fixture(t), "--json"}, strings.NewReader(""), &out, &stderr)
	if code != 0 || !json.Valid(out.Bytes()) || strings.Count(stderr.String(), "repocli: log warning:") != 1 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
	}
}

func TestLogWriteFailuresWarnOnce(t *testing.T) {
	for _, output := range []io.Writer{failingWriter{}, shortWriter{}} {
		var stderr bytes.Buffer
		writer := &logWriter{output: output, stderr: &stderr}
		for range 2 {
			if _, err := writer.Write([]byte("record\n")); err == nil {
				t.Fatal("missing write error")
			}
		}
		if strings.Count(stderr.String(), "repocli: log warning:") != 1 {
			t.Fatal(stderr.String())
		}
	}
}

func TestLogRetention(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 3, 15, 12, 0, 0, 0, time.FixedZone("local", 8*60*60))
	expired := now.AddDate(0, 0, -30).Format("2006-01-02") + ".log"
	kept := []string{now.Format("2006-01-02") + ".log", now.AddDate(0, 0, -29).Format("2006-01-02") + ".log", "2099-01-01.log", "notes.log", "2020-01-01.log.bak", "2020-99-99.log"}
	for _, name := range append(kept, expired) {
		put(t, dir, name, "record")
	}
	if err := os.Mkdir(filepath.Join(dir, "2020-01-01.log"), 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "2020-01-02.log")); err != nil {
		t.Fatal(err)
	}
	if err := pruneLogs(dir, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, expired)); !os.IsNotExist(err) {
		t.Fatalf("expired file: %v", err)
	}
	for _, name := range append(kept, "2020-01-01.log", "2020-01-02.log") {
		if _, err := os.Lstat(filepath.Join(dir, name)); err != nil {
			t.Errorf("removed %s: %v", name, err)
		}
	}
}

func TestConcurrentDiffLogs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := fixture(t)
	const runs = 6
	var wg sync.WaitGroup
	for range runs {
		wg.Go(func() {
			var out, stderr bytes.Buffer
			code := Execute(context.Background(), []string{"diff", "--repo", dir, "--json"}, strings.NewReader(""), &out, &stderr)
			if code != 0 || !json.Valid(out.Bytes()) || stderr.Len() != 0 {
				t.Errorf("code=%d stderr=%s", code, stderr.String())
			}
		})
	}
	wg.Wait()
	logs := readLogs(t, home)
	ids := map[string]int{}
	pattern := regexp.MustCompile(`run_id=([^ ]+)`)
	for line := range strings.SplitSeq(strings.TrimSpace(logs), "\n") {
		matches := pattern.FindAllStringSubmatch(line, -1)
		if !strings.HasPrefix(line, "[repocli] time=") || len(matches) != 1 {
			t.Fatalf("torn record: %s", line)
		}
		ids[matches[0][1]]++
	}
	if len(ids) != runs {
		t.Fatalf("run IDs: %v", ids)
	}
	for id, count := range ids {
		if count != 2 {
			t.Errorf("run %s has %d records", id, count)
		}
	}
}
