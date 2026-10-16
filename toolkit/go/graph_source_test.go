package repocli_test

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"slices"
	"testing"

	cg "github.com/compforge/codegraph"
	"github.com/compforge/repocli/toolkit/go"
)

type memoryGraphSource struct {
	files   map[string]string
	catalog []repocli.SourceFile
	reads   map[string]int
}

func (s *memoryGraphSource) SnapshotID() string { return "captured-input" }
func (s *memoryGraphSource) SourceFiles(context.Context) ([]repocli.SourceFile, error) {
	return s.catalog, nil
}
func (s *memoryGraphSource) ReadSource(ctx context.Context, name string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.reads[name]++
	if data, ok := s.files[name]; ok {
		return data, nil
	}
	return "", fs.ErrNotExist
}
func graphMemory(files map[string]string) *memoryGraphSource {
	s := &memoryGraphSource{files: files, reads: map[string]int{}}
	for name, data := range files {
		s.catalog = append(s.catalog, repocli.SourceFile{Path: name, Size: int64(len(data))})
	}
	return s
}
func sourceGraphOptions() repocli.GraphOptions {
	return repocli.GraphOptions{CodeGraph: cg.Options{MaxDocuments: 10, MaxDocumentBytes: 4096, MaxSourceBytes: 16384}, Select: func(name string, _ []cg.Tag) bool { return repocli.IsSourceMaterial(name) }}
}

// +case:id=source_graph_materials,desc=`Captured ignore rules, module declarations and priority share one view`,expect=`Cross-module bindings use CodeGraph facts and omitted materials remain unread with diagnostics`
func TestGraphFromSourceKeepsModulesPriorityAndIgnoreRules(t *testing.T) {
	source := graphMemory(map[string]string{
		"go.mod":        "module example.org/app\n",
		"lib/go.mod":    "module example.org/lib\n",
		"zz_changed.go": "package main\nimport \"example.org/lib\"\nfunc main(){ lib.Work() }\n",
		"lib/work.go":   "package lib\nfunc Work(){}\n",
		"aaa.go":        "package main\nfunc Background(){}\n",
		"ignored.go":    "package main\nfunc Ignored(){}\n",
		".gitignore":    "ignored.go\n",
	})
	original := slices.Clone(source.catalog)
	opts := sourceGraphOptions()
	opts.CodeGraph.MaxDocuments = 4
	opts.Prioritize = []string{"zz_changed.go", "lib/work.go"}
	result, err := repocli.GraphFromSource(context.Background(), source, opts)
	if err != nil {
		t.Fatal(err)
	}
	if result.Graph.Snapshot() != source.SnapshotID() || result.Documents != 4 {
		t.Fatalf("result: %+v", result)
	}
	for _, name := range []string{"go.mod", "lib/go.mod", "zz_changed.go", "lib/work.go"} {
		if _, ok := result.Sources[name]; !ok {
			t.Errorf("missing %s", name)
		}
	}
	if source.reads["ignored.go"] != 0 || source.reads["aaa.go"] != 0 {
		t.Fatal("omitted sources read", source.reads)
	}
	if !reflect.DeepEqual(source.catalog, original) {
		t.Fatal("input catalog mutated")
	}
	modules := map[string]bool{}
	call := false
	for _, node := range result.Graph.Nodes() {
		if node.Kind == cg.Module {
			modules[node.Name] = true
		}
	}
	for _, edge := range result.Graph.Relations() {
		if edge.Kind == cg.Calls {
			target, _ := result.Graph.Node(edge.Target)
			if target.Name == "Work" {
				call = true
			}
		}
	}
	if !modules["example.org/app"] || !modules["example.org/lib"] || !call {
		t.Fatalf("module/binding facts lost: %v calls=%v", modules, call)
	}
	if len(result.InputDiagnostics) != 1 || result.InputDiagnostics[0].Code != "document_limit" {
		t.Fatalf("coverage: %+v", result.InputDiagnostics)
	}
}

func TestGraphFromSourceBudgetReductionIsExplicit(t *testing.T) {
	source := graphMemory(map[string]string{"first.go": "package p\nfunc First(){}\n"})
	opts := sourceGraphOptions()
	one, err := repocli.GraphFromSource(context.Background(), source, opts)
	if err != nil {
		t.Fatal(err)
	}
	limit := len(one.Graph.Nodes())
	source = graphMemory(map[string]string{"first.go": "package p\nfunc First(){}\n", "second.go": "package p\nfunc Second(){}\n"})
	opts.CodeGraph.MaxNodes = limit
	opts.Prioritize = []string{"second.go"}
	if _, err := repocli.GraphFromSource(context.Background(), source, opts); !errors.Is(err, cg.ErrBuildBudget) {
		t.Fatalf("strict budget lost: %v", err)
	}
	opts.RetryOnBudget = true
	result, err := repocli.GraphFromSource(context.Background(), source, opts)
	if err != nil {
		t.Fatal(err)
	}
	if result.Documents != 1 || len(result.InputDiagnostics) != 1 || result.InputDiagnostics[0].Code != "graph_budget" {
		t.Fatalf("coverage: %+v", result)
	}
	if _, ok := result.Sources["second.go"]; !ok {
		t.Fatal("priority lost")
	}
	if _, ok := result.Sources["first.go"]; ok {
		t.Fatal("omitted bytes presented as graph source")
	}
}

func TestGraphFromSourceReportsUnavailableInputsAndCancellation(t *testing.T) {
	source := graphMemory(map[string]string{"ok.go": "package p\nfunc OK(){}\n"})
	source.catalog = append(source.catalog, repocli.SourceFile{Path: "missing.go", Size: 1}, repocli.SourceFile{Path: "large.go", Size: 5000})
	result, err := repocli.GraphFromSource(context.Background(), source, sourceGraphOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.InputDiagnostics) != 2 || source.reads["large.go"] != 0 {
		t.Fatalf("coverage=%+v reads=%v", result.InputDiagnostics, source.reads)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repocli.GraphFromSource(ctx, source, sourceGraphOptions()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	source.catalog = []repocli.SourceFile{{Path: "../.gitignore", Size: 1}}
	if _, err := repocli.GraphFromSource(context.Background(), source, sourceGraphOptions()); err == nil {
		t.Fatal("invalid path accepted")
	}
}

// +case:id=go_role_facts,desc=`Runtime main and init declarations can live outside main.go`,expect=`Roles use semantic entrypoint facts and reject filename-only guesses`
func TestSourceRolesConsumeGoEntrypointFacts(t *testing.T) {
	graph, _, err := cg.Build(context.Background(), "roles", []cg.Document{
		{Path: "cmd/run.go", Content: []byte("package main\nfunc main(){}\n")},
		{Path: "lib/main.go", Content: []byte("package lib\nfunc Helper(){}\n")},
		{Path: "lib/setup.go", Content: []byte("package lib\nfunc init(){}\n")},
	}, cg.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		entry bool
	}{{"cmd/run.go", true}, {"lib/main.go", false}, {"lib/setup.go", true}} {
		roles := repocli.SourceRoles(tc.name, graph)
		if !slices.Contains(roles, "source") || slices.Contains(roles, "entrypoint") != tc.entry {
			t.Errorf("%s: %v", tc.name, roles)
		}
	}
	if slices.Contains(repocli.SourceRoles("cmd/run.go", nil), "entrypoint") {
		t.Fatal("invented runtime entrypoint without graph")
	}
}

func TestSourceRolesPreserveRepositoryConventions(t *testing.T) {
	for _, tc := range []struct{ name, role string }{{"go.work", "manifest"}, {"go.work.sum", "lock"}, {"test_app.py", "test"}, {"conftest.py", "test"}, {"__main__.py", "entrypoint"}, {"web/main.ts", "entrypoint"}, {"src/__tests__/a.js", "test"}, {"tsconfig.build.json", "manifest"}} {
		if roles := repocli.SourceRoles(tc.name, nil); !slices.Contains(roles, tc.role) {
			t.Errorf("%s: %v", tc.name, roles)
		}
	}
	for _, name := range []string{"vendor/a.go", ".venv/lib/a.py", "node_modules/a.js", "testdata/a.go"} {
		if !repocli.IsSourceMaterial(name) {
			t.Errorf("source format lost for %s", name)
		}
	}
	source := graphMemory(map[string]string{"main.py": "from fastapi import FastAPI\napp=FastAPI()\n@app.get('/x')\ndef get_x(): return 1\n"})
	result, err := repocli.GraphFromSource(context.Background(), source, sourceGraphOptions())
	if err != nil {
		t.Fatal(err)
	}
	roles := repocli.SourceRoles("main.py", result.Graph)
	if !slices.Contains(roles, "handler") || !slices.Contains(roles, "entrypoint") {
		t.Fatal(roles)
	}
}

func TestOpenGraphSourceRetainsPinnedBytesAndObservesLiveDirectory(t *testing.T) {
	ctx := context.Background()
	root := fixture(t)
	pinned, err := repocli.OpenGraphSource(ctx, repocli.InputRequest{Repository: root, Head: "HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := pinned.ReadSource(ctx, "main.go")
	if err != nil {
		t.Fatal(err)
	}
	changed := "package main\nfunc main() { println(1) }\n"
	put(t, root, "main.go", changed)
	after, err := pinned.ReadSource(ctx, "main.go")
	if err != nil || after != before {
		t.Fatalf("pinned source changed: %q %v", after, err)
	}
	git(t, root, "add", "main.go")
	staged, err := repocli.OpenGraphSource(ctx, repocli.InputRequest{Repository: root, Staged: true})
	if err != nil {
		t.Fatal(err)
	}
	put(t, root, "main.go", "package main\nfunc main() {}\n")
	data, err := staged.ReadSource(ctx, "main.go")
	if err != nil || data != changed || staged.SnapshotID() == pinned.SnapshotID() {
		t.Fatalf("staged identity/bytes lost: %q %v", data, err)
	}

	// Unpacked source has no Git requirement and carries no immutable identity.
	directory := t.TempDir()
	live, err := repocli.OpenGraphSource(ctx, repocli.InputRequest{Repository: directory})
	if err != nil {
		t.Fatal(err)
	}
	put(t, directory, "main.go", changed)
	files, err := live.SourceFiles(ctx)
	if err != nil || len(files) != 1 || files[0].Path != "main.go" {
		t.Fatalf("live catalog: %+v %v", files, err)
	}
	data, err = live.ReadSource(ctx, "main.go")
	if err != nil || data != changed || live.SnapshotID() != "working-directory" {
		t.Fatalf("live view: %q %v", data, err)
	}
}
