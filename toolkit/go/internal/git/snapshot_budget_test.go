package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkingSnapshotByteBudget(t *testing.T) {
	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.go", "b.go"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("four"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	repository := Repository{Root: root, MaxSnapshotBytes: 7}
	snapshot := Snapshot{Files: map[string][]byte{}, Links: map[string]string{}, Opaque: map[string]string{}}
	if err := repository.readWorkingFiles(t.Context(), &snapshot, []string{"a.go", "b.go"}); err == nil || !strings.Contains(err.Error(), "limit=7") {
		t.Fatalf("budget error=%v", err)
	}
	repository.MaxSnapshotBytes = 8
	snapshot = Snapshot{Files: map[string][]byte{}, Links: map[string]string{}, Opaque: map[string]string{}}
	if err := repository.readWorkingFiles(t.Context(), &snapshot, []string{"a.go", "b.go"}); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Files) != 2 {
		t.Fatalf("captured files=%d", len(snapshot.Files))
	}
	repository.MaxSnapshotBytes = 0
	if repository.snapshotByteLimit() != 128<<20 {
		t.Fatal("default snapshot byte limit changed")
	}
}
