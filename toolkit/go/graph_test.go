package repocli_test

import (
	"context"
	"testing"

	shared "github.com/compforge/codegraph"
	"github.com/compforge/repocli/toolkit/go"
)

func TestGraphExcludesToolingTagsBeforeDocumentLimit(t *testing.T) {
	root := fixture(t)
	for _, dir := range []string{".idea", ".vscode", ".codebase", ".devloop", ".codex", ".claude", ".agents", ".cursor"} {
		put(t, root, dir+"/settings.json", "{}")
		put(t, root, "component/"+dir+"/helper.go", "package helper\nfunc Hidden() {}\n")
	}
	put(t, root, ".github/workflows/build.yml", "name: build\n")
	for _, name := range []string{"AGENTS.md", "CLAUDE.md", "GEMINI.md"} {
		put(t, root, name, "Project instructions")
		put(t, root, "component/"+name, "Component instructions")
	}
	put(t, root, ".idea-notes/README.md", "Ordinary documentation")
	git(t, root, "add", ".")
	req := repocli.InputRequest{Repository: root}
	snapshot, err := repocli.Snapshot(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := repocli.Graph(context.Background(), req, 5)
	if err != nil {
		t.Fatal(err)
	}
	if graph.Snapshot.Snapshot != snapshot.Snapshot {
		t.Fatal("graph filtering changed snapshot identity")
	}
	want := []string{"go.mod", "main.go", "testdata/corpus/go.mod", ".github/workflows/build.yml", ".idea-notes/README.md"}
	if graph.Documents != len(want) || len(graph.Sources) != len(want) {
		t.Fatalf("unexpected graph document count: %d, sources: %d", graph.Documents, len(graph.Sources))
	}
	for _, path := range want {
		if _, ok := graph.Sources[path]; !ok {
			t.Errorf("missing document %s", path)
		}
	}
	for _, d := range graph.Diagnostics {
		if d.Code == "document_limit" {
			t.Fatal("excluded metadata consumed the document budget")
		}
	}
}

func TestGraphUsesCapturedIgnoreRulesForTrackedFiles(t *testing.T) {
	root := fixture(t)
	put(t, root, "generated/drop.go", "package generated\nfunc Drop() {}\n")
	put(t, root, "generated/keep.go", "package generated\nfunc Keep() {}\n")
	put(t, root, "nested/drop.go", "package nested\nfunc Drop() {}\n")
	put(t, root, ".gitignore", "generated/*.go\n!generated/keep.go\n")
	put(t, root, "nested/.gitignore", "drop.go\n")
	git(t, root, "add", "-f", ".")
	git(t, root, "commit", "-qm", "tracked ignored files")
	// Change only the worktree rules: index and commit graphs must keep using
	// their own rules rather than the current filesystem's .gitignore.
	put(t, root, ".gitignore", "generated/keep.go\n")
	put(t, root, "nested/.gitignore", "")
	for _, tc := range []struct {
		name         string
		req          repocli.InputRequest
		dropIncluded bool
	}{
		{"working", repocli.InputRequest{Repository: root}, true},
		{"index", repocli.InputRequest{Repository: root, Staged: true}, false},
		{"commit", repocli.InputRequest{Repository: root, Head: "HEAD"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			graph, err := repocli.Graph(context.Background(), tc.req, 100)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"generated/drop.go", "nested/drop.go"} {
				if _, ok := graph.Sources[name]; ok != tc.dropIncluded {
					t.Errorf("%s included = %v", name, ok)
				}
			}
			if _, ok := graph.Sources["generated/keep.go"]; ok == tc.dropIncluded {
				t.Errorf("negated path included = %v", ok)
			}
		})
	}
}

// +case:id=graph_source_materials,desc=`Only source material receives symbol parsing`,expect=`Configuration and documentation remain context without parser diagnostics`
func TestGraphSkipsNonSourceSymbolParsing(t *testing.T) {
	root := fixture(t)
	materials := map[string]string{
		".editorconfig":     "invalid editor configuration",
		".dockerignore":     "build/\n",
		".gitignore":        "build/\n",
		".env.example":      "APP_MODE=example\n",
		"nested/.env.local": "APP_MODE=local\n",
		"README.md":         "# Guide\n",
		"assets/data.json":  "invalid json",
		"notes.unknown":     "unknown material",
	}
	for name, content := range materials {
		put(t, root, name, content)
	}
	git(t, root, "add", ".")
	graph, err := repocli.Graph(context.Background(), repocli.InputRequest{Repository: root}, 100)
	if err != nil {
		t.Fatal(err)
	}
	for name := range materials {
		if _, ok := graph.Sources[name]; !ok {
			t.Errorf("missing material %s", name)
		}
	}
	documents := map[string]bool{}
	hasSourceSymbol, hasManifest := false, false
	for _, n := range graph.Nodes {
		if n.Kind == shared.DocumentNodeKind {
			documents[n.Path] = true
			if n.Path == "go.mod" && n.Manifest != nil {
				hasManifest = true
			}
		}
		if n.Kind == shared.Function && n.Location != nil && n.Location.Path == "main.go" {
			hasSourceSymbol = true
		}
		if n.Location != nil {
			if _, ok := materials[n.Location.Path]; ok && n.Kind != shared.DocumentNodeKind {
				t.Errorf("non-source material contributed symbol: %+v", n)
			}
		}
	}
	for name := range materials {
		if !documents[name] {
			t.Errorf("missing document node %s", name)
		}
	}
	if !hasSourceSymbol || !hasManifest {
		t.Fatal("source parsing or manifest metadata was lost")
	}
	for _, d := range graph.Diagnostics {
		if _, ok := materials[d.Location.Path]; ok {
			t.Errorf("non-source parser diagnostic: %+v", d)
		}
	}
}
