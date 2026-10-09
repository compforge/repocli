package impact

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	shared "github.com/compforge/codegraph"
	"github.com/compforge/repocli/toolkit/go/internal/codegraph"
	"github.com/compforge/repocli/toolkit/go/internal/diff"
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

func TestWorksetsRebindUnchangedCallerWhenTargetChanges(t *testing.T) {
	before := map[string][]byte{
		"caller.go": []byte("package app\nfunc Caller(){ Target() }\n"),
		"target.go": []byte("package app\nfunc Target(){}\n"),
	}
	after := map[string][]byte{
		"caller.go": before["caller.go"],
		"target.go": []byte("package app\nfunc Other(){}\n"),
	}
	builds, err := buildWorksets(context.Background(), Request{Before: before, After: after,
		Changes: []diff.Change{{Path: "target.go"}}, TestDirs: []string{"."}}, []string{"caller.go"})
	if err != nil {
		t.Fatal(err)
	}
	caller := codegraph.SymbolID("caller.go", "Caller")
	calls := func(b codegraph.BuildResult) []codegraph.Relation {
		var out []codegraph.Relation
		for _, r := range b.Graph.Outgoing(caller) {
			if r.Kind == codegraph.Calls {
				out = append(out, r)
			}
		}
		return out
	}
	if len(calls(builds[0])) != 1 || len(calls(builds[1])) != 0 {
		t.Fatalf("stale binding: before=%+v after=%+v", calls(builds[0]), calls(builds[1]))
	}
	for i, files := range []map[string][]byte{before, after} {
		fresh, err := codegraph.Build(context.Background(), codegraph.BuildRequest{BuildOptions: codegraph.BuildOptions{
			Files: files, Kinds: append(append([]codegraph.Kind{}, impactKinds...), codegraph.ConfigScope), MaxDepth: 32, MaxFiles: DefaultMaxFiles, MaxRelations: DefaultMaxRelations,
		}, FilesToExpand: []string{"target.go", "caller.go"}})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(builds[i].Sources, fresh.Sources) || !reflect.DeepEqual(builds[i].Diagnostics, fresh.Diagnostics) || !reflect.DeepEqual(builds[i].Graph.Nodes, fresh.Graph.Nodes) || !reflect.DeepEqual(calls(builds[i]), calls(fresh)) {
			t.Fatalf("cached side %d differs from independent construction", i)
		}
	}
}

func TestWorksetFileBudgetDefaultsAndCallerExpansion(t *testing.T) {
	files := make(map[string][]byte, 10001)
	for i := range 10001 {
		files[fmt.Sprintf("file%05d.ts", i)] = []byte("// source\n")
	}
	for _, limit := range []int{0, 10001} {
		builds, err := buildWorksets(context.Background(), Request{Before: files, After: files, MaxFiles: limit}, nil)
		if err != nil {
			t.Fatal(err)
		}
		want := 10000
		if limit != 0 {
			want = limit
		}
		for side, built := range builds {
			if len(built.ParsedFiles) != want {
				t.Fatalf("side %d: parsed %d files, want %d", side, len(built.ParsedFiles), want)
			}
			if limit != 0 {
				if len(built.Diagnostics) != 0 {
					t.Fatalf("expanded budget still incomplete: %+v", built.Diagnostics)
				}
				continue
			}
			if len(built.Diagnostics) != 1 || built.Diagnostics[0].Code != "file_limit" || built.Diagnostics[0].Path != "file10000.ts" {
				t.Fatalf("unexpected overflow diagnostics: %+v", built.Diagnostics)
			}
			for _, detail := range []string{"parsed=10000 limit=10000 omitted=1", "Go heap allocation=", "not RSS or peak", "increase MaxFiles"} {
				if !strings.Contains(built.Diagnostics[0].Message, detail) {
					t.Fatalf("missing %q: %s", detail, built.Diagnostics[0].Message)
				}
			}
		}
	}
}

func TestWorksetRelationBudgetReportsMemoryAndAllowsRetry(t *testing.T) {
	files := map[string][]byte{"a.ts": []byte("export function a() { return 1; }")}
	req := Request{Before: files, After: files, MaxRelations: 1}
	_, err := buildWorksets(context.Background(), req, nil)
	if !errors.Is(err, shared.ErrBuildBudget) {
		t.Fatalf("expected graph budget failure, got %v", err)
	}
	for _, detail := range []string{"before workset", "parsed files=1", "MaxRelations=1", "Go heap allocation=", "not RSS or peak"} {
		if !strings.Contains(err.Error(), detail) {
			t.Fatalf("missing %q: %v", detail, err)
		}
	}
	req.MaxRelations = 1000
	if _, err := buildWorksets(context.Background(), req, nil); err != nil {
		t.Fatalf("caller-expanded budget failed: %v", err)
	}
}

func TestFileBudgetPreservesKnownImpactAndMarksUnknown(t *testing.T) {
	files := map[string][]byte{"a.ts": []byte("export const a = 1;"), "b.ts": []byte("import './a';")}
	result, err := Analyze(context.Background(), Request{Before: files, After: files, MaxFiles: 1,
		Changes: []diff.Change{{Path: "a.ts", Status: "modified"}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Scope != "partial" || len(result.AffectedFiles) == 0 {
		t.Fatalf("lost known impact or marked truncated analysis complete: %+v", result)
	}
	if len(result.Uncertainties) != 2 {
		t.Fatalf("expected before/after budget gaps: %+v", result.Uncertainties)
	}
	for _, gap := range result.Uncertainties {
		if gap.Reason != "file_limit" || gap.Scope != "repository" {
			t.Fatalf("unparsed consumers must remain unknown: %+v", gap)
		}
	}
}
