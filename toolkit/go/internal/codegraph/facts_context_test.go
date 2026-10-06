package codegraph

import (
	"context"
	"reflect"
	"slices"
	"testing"

	shared "github.com/compforge/codegraph"
)

func TestFactsContextConfigChangeRebindsWithSharedExtractor(t *testing.T) {
	cache, err := shared.NewExtractionCache(100, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	extractor, err := shared.NewExtractor(shared.ExtractionOptions{Cache: cache})
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"main.ts":    []byte("import {work} from '@lib/work'; export function caller(){work();}"),
		"v1/work.ts": []byte("export function work(){}"),
		"v2/work.ts": []byte("export function work(){}"),
	}
	build := func(root string, e *shared.Extractor) *Builder {
		t.Helper()
		files["package.json"] = []byte(`{"name":"@lib/work","exports":"./` + root + `/work.ts"}`)
		b, err := NewBuilder(context.Background(), BuildOptions{Files: files, Kinds: []Kind{Imports, Calls}, MaxDepth: 10, MaxFiles: 10, Extractor: e})
		if err != nil {
			t.Fatal(err)
		}
		if err := b.Add(context.Background(), "main.ts", "v1/work.ts", "v2/work.ts"); err != nil {
			t.Fatal(err)
		}
		calls := b.sourceGraph.RelationsFrom(b.sourceGraph.Find("main.ts", shared.Function, "caller")[0].ID, shared.Calls)
		if len(calls) != 1 {
			t.Fatalf("config target not bound: %+v", b.sourceGraph.Report())
		}
		target, _ := b.sourceGraph.Node(calls[0].Target)
		if target.Location.Path != root+"/work.ts" {
			t.Fatalf("stale config binding: %+v", target)
		}
		return b
	}
	before := build("v1", extractor)
	after := build("v2", extractor)
	fresh := build("v2", nil)
	afterResult, freshResult := after.Result(), fresh.Result()
	// Result projects a relation map into adjacency slices. Compare all content
	// in query order so map iteration cannot masquerade as a stale binding.
	for _, result := range []BuildResult{afterResult, freshResult} {
		for _, relations := range result.Graph.outgoing {
			slices.SortFunc(relations, compareRelations)
		}
		for _, relations := range result.Graph.incoming {
			slices.SortFunc(relations, compareRelations)
		}
	}
	if !reflect.DeepEqual(after.sourceGraph.Nodes(), fresh.sourceGraph.Nodes()) || !reflect.DeepEqual(after.sourceGraph.Relations(), fresh.sourceGraph.Relations()) || !reflect.DeepEqual(afterResult, freshResult) {
		t.Fatal("cached config change differs from fresh analysis")
	}
	original := before.sourceGraph.RelationsFrom(before.sourceGraph.Find("main.ts", shared.Function, "caller")[0].ID, shared.Calls)
	target, _ := before.sourceGraph.Node(original[0].Target)
	if target.Location.Path != "v1/work.ts" {
		t.Fatal("before graph changed after another build")
	}
}

func TestFactsPythonLoopContextAndInitializerSeparation(t *testing.T) {
	files := map[string][]byte{
		"main.py":           []byte("from pathlib import Path\nimport sys\nROOT = Path(__file__).parent\nfor root in [ROOT / 'a', ROOT / 'b']:\n    sys.path.insert(0, str(root))\n    from pkg.work import run\ndef caller():\n    return run()\n"),
		"a/pkg/__init__.py": []byte("def run():\n    pass\n"),
		"a/pkg/work.py":     []byte("def run():\n    pass\n"),
		"b/pkg/__init__.py": []byte("def run():\n    pass\n"),
		"b/pkg/work.py":     []byte("def run():\n    pass\n"),
	}
	b, err := NewBuilder(context.Background(), BuildOptions{Files: files, Kinds: []Kind{Imports, Calls}, MaxDepth: 10, MaxFiles: 10})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Add(context.Background(), "main.py"); err != nil {
		t.Fatal(err)
	}
	calls := b.sourceGraph.RelationsFrom(b.sourceGraph.Find("main.py", shared.Function, "caller")[0].ID, shared.Calls)
	if len(calls) != 2 {
		t.Fatalf("loop candidates: %+v; context: %+v", calls, b.resolution)
	}
	for _, call := range calls {
		target, _ := b.sourceGraph.Node(call.Target)
		if target.Location.Path != "a/pkg/work.py" && target.Location.Path != "b/pkg/work.py" {
			t.Fatalf("bound an incidental initializer: %+v", target)
		}
	}
}
