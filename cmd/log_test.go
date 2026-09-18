package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
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
	report := runJSON(t, args, "")
	if report.Scope != "focused" || len(report.SourceFiles) != 1 || len(report.TestFiles) != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	logs := readLogs(t, home)
	for _, want := range []string{"[repocli] time=", "msg=command.started", "version=" + Version, "args=", "msg=command.finished", "command=\"repocli diff\"", "exit_code=0"} {
		if !strings.Contains(logs, want) {
			t.Errorf("missing %q in %s", want, logs)
		}
	}
	ids := regexp.MustCompile(`run_id=([^ ]+)`).FindAllStringSubmatch(logs, -1)
	if len(ids) != 3 || ids[0][1] != ids[1][1] || ids[1][1] != ids[2][1] {
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
	if !strings.Contains(logs, "msg=command.stdout") {
		t.Fatal(logs)
	}
	for _, d := range result.Diagnostics {
		if !strings.Contains(logs, d.Code) {
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
			for _, want := range []string{"msg=command.failed", "level=ERROR", "exit_code=1", "msg=command.stderr", "error="} {
				if !strings.Contains(logs, want) {
					t.Errorf("missing %s: %s", want, logs)
				}
			}
			if strings.Contains(logs, "msg=command.finished") {
				t.Fatal(logs)
			}

		})
	}
}

func TestAllCommandOutcomesAreLogged(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
	}{
		{nil, 0}, {[]string{"--help"}, 0}, {[]string{"help", "diff"}, 0},
		{[]string{"diff", "--help"}, 0}, {[]string{"version"}, 0}, {[]string{"--version"}, 0},
		{[]string{"missing-command"}, 2}, {[]string{"--invalid"}, 2},
		{[]string{"diff", "--no-log"}, 2},
		{[]string{"diff", "--timeout", "0s"}, 2},
		{[]string{"diff", "--timeout", "invalid"}, 2},
		{[]string{"diff", "--head", "HEAD", "--staged"}, 2},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			var stdout, stderr bytes.Buffer
			code := Execute(context.Background(), tc.args, strings.NewReader(""), &stdout, &stderr)
			if code != tc.code {
				t.Fatalf("code %d: %s", code, stderr.String())
			}
			logs := readLogs(t, home)
			for _, want := range []string{"msg=command.started", fmt.Sprintf("exit_code=%d", code)} {
				if !strings.Contains(logs, want) {
					t.Errorf("missing %q: %s", want, logs)
				}
			}
			if code == 0 {
				if !strings.Contains(logs, "msg=command.finished") || !strings.Contains(logs, "msg=command.stdout") {
					t.Fatal(logs)
				}
			} else {
				if !strings.Contains(logs, "msg=command.failed") || !strings.Contains(logs, "msg=command.stderr") {
					t.Fatal(logs)
				}
			}
			assertLogStreams(t, logs, stdout.String(), stderr.String())
		})
	}
}

// A future command needs no log setup, hooks, or awareness of the log type.
func TestNewCommandInheritsLogging(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var stdout, stderr bytes.Buffer
	root := newRootCommand(&options{})
	root.AddCommand(&cobra.Command{Use: "future", RunE: func(command *cobra.Command, _ []string) error {
		fmt.Fprintln(command.OutOrStdout(), "future output")
		fmt.Fprintln(command.ErrOrStderr(), "future warning")
		return executionError{fmt.Errorf("future failure")}
	}})
	code := execute(context.Background(), root, []string{"future"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("code=%d", code)
	}
	logs := readLogs(t, home)
	if !strings.Contains(logs, `command="repocli future"`) || !strings.Contains(logs, `error="future failure"`) {
		t.Fatal(logs)
	}
	assertLogStreams(t, logs, stdout.String(), stderr.String())
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
		if count != 3 {
			t.Errorf("run %s has %d records", id, count)
		}
	}
}

func TestLogMirrorsOutputStreams(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			dir := fixture(t)
			var stdout, stderr bytes.Buffer
			var output io.Writer = &stdout
			if fail {
				output = failingWriter{}
			}
			code := Execute(context.Background(), []string{"diff", "--repo", dir, "--json"}, strings.NewReader(""), output, &stderr)
			if (code != 0) != fail {
				t.Fatalf("code=%d: %s", code, stderr.String())
			}
			logs := readLogs(t, home)
			assertLogStreams(t, logs, stdout.String(), stderr.String())
		})
	}
}

func assertLogStreams(t *testing.T, logs, stdout, stderr string) {
	t.Helper()
	for stream, want := range map[string]string{"stdout": stdout, "stderr": stderr} {
		var got strings.Builder
		for line := range strings.SplitSeq(logs, "\n") {
			if !strings.Contains(line, "msg=command."+stream+" ") {
				continue
			}
			_, value, ok := strings.Cut(line, " data=")
			if !ok {
				t.Fatalf("missing stream data: %s", line)
			}
			decoded, err := strconv.Unquote(value)
			if err != nil {
				t.Fatal(err)
			}
			got.WriteString(decoded)
		}
		if got.String() != want {
			t.Fatalf("%s log differs: got %q want %q", stream, got.String(), want)
		}
	}
}
