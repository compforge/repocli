package impact

import (
	"context"
	"reflect"
	"slices"
	"testing"

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
			Files: files, Kinds: append(append([]codegraph.Kind{}, impactKinds...), codegraph.ConfigScope), MaxDepth: 32, MaxFiles: 2000,
		}, FilesToExpand: []string{"target.go", "caller.go"}})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(builds[i].Sources, fresh.Sources) || !reflect.DeepEqual(builds[i].Diagnostics, fresh.Diagnostics) || !reflect.DeepEqual(builds[i].Graph.Nodes, fresh.Graph.Nodes) || !reflect.DeepEqual(calls(builds[i]), calls(fresh)) {
			t.Fatalf("cached side %d differs from independent construction", i)
		}
	}
}
