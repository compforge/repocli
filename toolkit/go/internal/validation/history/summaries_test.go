package history

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tidwall/wal"
)

// +case:id=stats_retained_repositories,desc=`Stored repositories have no live checkout and include an old run plus corrupt history`,expect=`Replay valid stores, preserve old data, and diagnose the corrupt store without partial counts`
func TestReadSummariesPreservesHistoryAndReportsGaps(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	for _, name := range []string{"a", "b"} {
		h := newStore(dir, name, "/missing-checkout")
		events := testEvents(time.Now().AddDate(0, 0, -60))
		if err := h.Append(ctx, events); err != nil {
			t.Fatal(err)
		}
	}
	bad := newStore(dir, "corrupt", "/missing-checkout")
	if err := bad.Append(ctx, testEvents(time.Now())); err != nil {
		t.Fatal(err)
	}
	if err := bad.access(ctx, func(log *wal.Log) error { return log.Write(3, []byte(`{"schemaVersion":99}`)) }); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "pr-cache", "prs"), 0700); err != nil {
		t.Fatal(err)
	}
	result, err := ReadSummaries(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Repositories) != 2 || len(result.Diagnostics) != 1 {
		t.Fatalf("%+v", result)
	}
	for _, summary := range result.Repositories {
		if c := summary.Checks[". lint"]; c.Executions != 1 || c.Passed != 1 || c.ExecutionSeconds != 2 {
			t.Fatalf("%+v", c)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "pr-cache", "lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cache changed: %v", err)
	}
	if _, err := bad.Read(ctx); err == nil {
		t.Fatal("corruption was silently repaired")
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ReadSummaries(ctx, dir); !errors.Is(err, context.Canceled) {
		t.Fatalf("%v", err)
	}
}

func TestReadSummariesMissingDirectoryDoesNotCreateIt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	result, err := ReadSummaries(context.Background(), dir)
	if err != nil || len(result.Repositories) != 0 {
		t.Fatalf("%+v %v", result, err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created directory: %v", err)
	}
}
