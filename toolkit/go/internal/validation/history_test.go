package validation

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tidwall/wal"
)

func testEvents(at time.Time) []historyEvent {
	id, inputs := rand.Text(), rand.Text()
	result := CheckResult{RunID: id, CheckSpec: CheckSpec{Check: "lint"}, Status: "passed", CheckedAt: at, DurationSeconds: 2}
	start := historyRecord("started", "/fixture", ".", inputs, result, false)
	end := historyRecord("finished", "/fixture", ".", inputs, result, true)
	start.At, end.At = at, at
	return []historyEvent{start, end}
}

// +case:id=history_wal_roll_reopen,desc=`History spans multiple WAL segments`,expect=`Reopening reconstructs identical run counts and durations`
func TestHistoryRollingAndReplay(t *testing.T) {
	h := newHistoryStore(t.TempDir(), "fixture", "/fixture")
	h.segmentSize = 128
	events := testEvents(time.Now().UTC())
	if err := h.append(context.Background(), events); err != nil {
		t.Fatal(err)
	}
	reuse := events[1]
	reuse.Type = "reused"
	reuse.Check.LookupSeconds = .1
	if err := h.append(context.Background(), []historyEvent{reuse}); err != nil {
		t.Fatal(err)
	}
	state, err := h.read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	counts := state.Summary.Checks[". lint"]
	if counts.Executions != 1 || counts.Passed != 1 || counts.Reuses != 1 || counts.ExecutionSeconds != 2 || counts.LookupSeconds != .1 || counts.Unfinished != 0 {
		t.Fatalf("%+v", counts)
	}
	files, err := os.ReadDir(filepath.Join(h.dir, "history"))
	if err != nil || len(files) < 2 {
		t.Fatalf("segments=%d: %v", len(files), err)
	}
}

func TestHistoryInterruptedAndCorruptTail(t *testing.T) {
	h := newHistoryStore(t.TempDir(), "fixture", "/fixture")
	events := testEvents(time.Now().UTC())
	if err := h.append(context.Background(), events[:1]); err != nil {
		t.Fatal(err)
	}
	state, err := h.read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c := state.Summary.Checks[". lint"]; c.Unfinished != 1 || c.Passed != 0 {
		t.Fatalf("%+v", c)
	}
	files, err := os.ReadDir(filepath.Join(h.dir, "history"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(h.dir, "history", files[len(files)-1].Name())
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"index":`); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if _, err := h.read(context.Background()); !errors.Is(err, wal.ErrCorrupt) {
		t.Fatalf("wanted corruption: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Fatal("history repaired without authorization")
	}
}

func TestHistoryRetentionPreservesReferences(t *testing.T) {
	h := newHistoryStore(t.TempDir(), "fixture", "/fixture")
	old := testEvents(time.Now().AddDate(0, 0, -60))
	retained := testEvents(time.Now().AddDate(0, 0, -45))
	recent := testEvents(time.Now())
	reuse := retained[1]
	reuse.Type = "reused"
	reuse.At = time.Now()
	events := append(append(append([]historyEvent{}, old...), retained...), recent...)
	events = append(events, reuse)
	if err := h.append(context.Background(), events); err != nil {
		t.Fatal(err)
	}
	state, err := h.maintain(context.Background(), 30)
	if err != nil {
		t.Fatal(err)
	}
	if state.Runs[old[0].Run] != nil || state.Runs[retained[0].Run] == nil {
		t.Fatal("retention lost a referenced run or retained unreferenced prefix")
	}
	if c := state.Summary.Checks[". lint"]; c.Executions != 2 || c.Reuses != 1 {
		t.Fatalf("%+v", c)
	}
	// Expiring everything must produce an empty log that can accept new runs.
	if _, err := h.maintain(context.Background(), -1); err != nil {
		t.Fatal(err)
	}
	if err := h.append(context.Background(), testEvents(time.Now())); err != nil {
		t.Fatal(err)
	}
	state, err = h.read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Summary.Checks[". lint"].Executions != 1 {
		t.Fatal(state.Summary)
	}
}

// +case:id=history_multiprocess_append,desc=`Independent CLI processes append to one checkout`,expect=`OS lock preserves all starts and completions across WAL reopen`
func TestHistoryMultipleProcesses(t *testing.T) {
	root := t.TempDir()
	processes := []*exec.Cmd{}
	for range 4 {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHistoryWriterProcess$")
		cmd.Env = append(os.Environ(), "REPOKIT_HISTORY_TEST="+root)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		processes = append(processes, cmd)
	}
	for _, cmd := range processes {
		if err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	state, err := newHistoryStore(root, "fixture", "/fixture").read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if counts := state.Summary.Checks[". lint"]; counts.Executions != 20 || counts.Passed != 20 || counts.Unfinished != 0 {
		t.Fatalf("%+v", counts)
	}
}

func TestHistoryWriterProcess(t *testing.T) {
	root := os.Getenv("REPOKIT_HISTORY_TEST")
	if root == "" {
		t.Skip("subprocess fixture")
	}
	h := newHistoryStore(root, "fixture", "/fixture")
	h.segmentSize = 512
	for range 5 {
		if err := h.append(context.Background(), testEvents(time.Now())); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHistoryLockCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	unlock, err := lockHistory(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if release, err := lockHistory(ctx, path); err == nil {
		release()
		t.Fatal("second writer acquired lock")
	} else if !strings.Contains(err.Error(), "deadline") {
		t.Fatal(err)
	}
}

// +case:id=history_retention_during_reuse,desc=`Another session expires a selected Run before reuse is persisted`,expect=`No dangling reference is written and unrelated completions are retained`
func TestHistoryRetentionDuringReuse(t *testing.T) {
	ctx := context.Background()
	h := newHistoryStore(t.TempDir(), "fixture", "/fixture")
	old := testEvents(time.Now().AddDate(0, 0, -40))
	if err := h.append(ctx, old); err != nil {
		t.Fatal(err)
	}
	selected, err := h.maintain(ctx, 60)
	if err != nil || selected.Runs[old[0].Run] == nil {
		t.Fatalf("selection: %v", err)
	}
	current := testEvents(time.Now())
	if err := h.append(ctx, current[:1]); err != nil {
		t.Fatal(err)
	}
	if _, err := h.maintain(ctx, 30); err != nil {
		t.Fatal(err)
	}
	reuse := old[1]
	reuse.Type, reuse.At = "reused", time.Now()
	if err := h.append(ctx, []historyEvent{reuse, current[1]}); err != nil {
		t.Fatal(err)
	}
	// A batch consisting entirely of expired reuses must also be harmless.
	if err := h.append(ctx, []historyEvent{reuse}); err != nil {
		t.Fatal(err)
	}
	state, err := h.maintain(ctx, 30)
	if err != nil {
		t.Fatal(err)
	}
	counts := state.Summary.Checks[". lint"]
	if counts.Executions != 1 || counts.Passed != 1 || counts.Unfinished != 0 || counts.Reuses != 0 {
		t.Fatalf("%+v", counts)
	}
	if state.Runs[old[0].Run] != nil {
		t.Fatal("expired run resurrected")
	}
}

// +case:id=history_invalid_batch_atomic,desc=`A batch contains an invalid completion after a valid start`,expect=`The entire batch is rejected without corrupting persisted history`
func TestHistoryRejectsInvalidBatch(t *testing.T) {
	ctx := context.Background()
	h := newHistoryStore(t.TempDir(), "fixture", "/fixture")
	original := testEvents(time.Now())
	if err := h.append(ctx, original); err != nil {
		t.Fatal(err)
	}
	next := testEvents(time.Now())
	invalid := next[1]
	invalid.Inputs = rand.Text()
	if err := h.append(ctx, []historyEvent{next[0], invalid}); err == nil {
		t.Fatal("invalid batch accepted")
	}
	state, err := h.read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Runs) != 1 || state.Runs[next[0].Run] != nil {
		t.Fatal("partial batch persisted")
	}
	if err := h.append(ctx, next); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryRepositoryNames(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", args, err, output)
		}
	}
	git("init", "-q")
	base := t.TempDir()
	paths := map[string]bool{}
	for _, origin := range []string{
		"https://github.com/one/repo.git", "https://github.com/two/repo.git",
		"https://gitlab.com/one/repo.git", "https://github.com/one--two/repo.git",
		"https://github.com/one/two/repo.git", "https://github.com/one-/repo.git", "https://github.com/one/-repo.git",
	} {
		git("config", "remote.origin.url", origin)
		h, err := openHistoryStore(ctx, base, root)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Dir(h.dir) != base || paths[h.dir] {
			t.Fatalf("unsafe or colliding directory: %s", h.dir)
		}
		paths[h.dir] = true
	}
	git("config", "remote.origin.url", "https://user:secret@github.com/example/repo.git?token=hidden")
	first, err := openHistoryStore(ctx, base, root)
	if err != nil {
		t.Fatal(err)
	}
	git("config", "remote.origin.url", "git@github.com:example/repo.git")
	second, err := openHistoryStore(ctx, base, root)
	if err != nil || first.dir != second.dir || filepath.Base(first.dir) != "github--example--repo" {
		t.Fatalf("%+v %+v %v", first, second, err)
	}
}

func TestHistoryRejectsCrossCheckoutEvents(t *testing.T) {
	h := newHistoryStore(t.TempDir(), "fixture", "/fixture")
	ctx := context.Background()
	events := testEvents(time.Now())
	if err := h.append(ctx, events); err != nil {
		t.Fatal(err)
	}
	reuse := events[1]
	reuse.Type, reuse.Checkout = "reused", "/other"
	if err := h.append(ctx, []historyEvent{reuse}); err == nil {
		t.Fatal("cross-checkout reference accepted")
	}
	state, err := h.read(ctx)
	if err != nil || state.Summary.Checks[". lint"].Reuses != 0 {
		t.Fatalf("%+v %v", state, err)
	}
}
