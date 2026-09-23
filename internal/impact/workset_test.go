package impact

import (
	"context"
	"slices"
	"testing"

	"github.com/compforge/repocli/internal/diff"
)

func TestWorksetIncludesChangedRootsTestsAndIntermediateDocuments(t *testing.T) {
	files := map[string][]byte{
		"changed.ts":   []byte("export const value = 1;"),
		"alone.ts":     []byte("export const other = 1;"),
		"bridge.ts":    []byte("export {value} from './changed';"),
		"api.test.ts":  []byte("import {value} from './bridge';"),
		"unrelated.ts": []byte("invalid ((("),
	}
	builds, err := buildWorksets(context.Background(), Request{Before: files, After: files,
		Changes: []diff.Change{{Path: "changed.ts"}, {Path: "alone.ts"}}, TestDirs: []string{"."}}, []string{"api.test.ts"})
	if err != nil {
		t.Fatal(err)
	}
	for _, built := range builds {
		if !slices.Equal(built.ParsedFiles, []string{"alone.ts", "api.test.ts", "bridge.ts", "changed.ts"}) {
			t.Fatalf("unexpected workset: %v", built.ParsedFiles)
		}
		if len(built.Sources["alone.ts"].Symbols) != 1 || len(built.Diagnostics) != 0 {
			t.Fatalf("changed root missing facts or unrelated source parsed: %+v", built)
		}
	}
}

func TestWorksetUsesVersionSpecificRenameDocuments(t *testing.T) {
	builds, err := buildWorksets(context.Background(), Request{
		Before:  map[string][]byte{"old.ts": []byte("export function old() {}")},
		After:   map[string][]byte{"new.ts": []byte("export function newName() {}")},
		Changes: []diff.Change{{Path: "new.ts", OldPath: "old.ts", Status: "renamed"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(builds[0].ParsedFiles, []string{"old.ts"}) || !slices.Equal(builds[1].ParsedFiles, []string{"new.ts"}) {
		t.Fatalf("mixed versions: %v / %v", builds[0].ParsedFiles, builds[1].ParsedFiles)
	}
}
