package codegraph

import (
	"context"
	"reflect"
	"slices"
	"testing"

	shared "github.com/compforge/codegraph"
)

func TestLanguageRequiresDeclarationCapability(t *testing.T) {
	for file, want := range map[string]string{
		"main.go": "go", "Main.java": "java", "main.rs": "rust",
		"data.json": "", "README.md": "", "unknown.repokit": "",
	} {
		if got := Language(file); got != want {
			t.Fatalf("Language(%q) = %q, want %q", file, got, want)
		}
	}
}

func TestSharedSourceExposesOutlineLanguagesAsPartial(t *testing.T) {
	source := sourceFacts(t, "Main.java", []byte("class Main { void run() {} }")).Source
	if source.Language != "java" || len(source.Symbols) != 2 || source.Symbols[0].Kind != "class" || source.Symbols[1].Kind != "method" {
		t.Fatalf("source = %+v", source)
	}
	if len(source.Diagnostics) != 1 || source.Diagnostics[0].Code != "unsupported_resolution" {
		t.Fatalf("issues = %+v", source.Diagnostics)
	}
}

func TestSharedSourceMapsDeclarationsAndExactCalls(t *testing.T) {
	source := []byte(`package sample
type Box struct { Value int }
func (b Box) Run() { Work() }
func Work() {}
`)
	facts := sourceFacts(t, "sample.go", source)
	wantSymbols := []Symbol{
		{QualifiedName: "Box", Name: "Box", Kind: "struct", StartLine: 2, EndLine: 2},
		{QualifiedName: "Box.Value", Name: "Value", Kind: "field", StartLine: 2, EndLine: 2},
		{QualifiedName: "Box.Run", Name: "Run", Kind: "method", StartLine: 3, EndLine: 3},
		{QualifiedName: "Work", Name: "Work", Kind: "function", StartLine: 4, EndLine: 4},
	}
	if !reflect.DeepEqual(facts.Symbols, wantSymbols) {
		t.Fatalf("symbols = %+v, want %+v", facts.Symbols, wantSymbols)
	}
	if facts.parents["Box.Value"] != "Box" || facts.parents["Box.Run"] != "Box" {
		t.Fatalf("parents = %+v", facts.parents)
	}
	calls := slices.DeleteFunc(slices.Clone(facts.dependencies), func(r Relation) bool { return r.Kind != Calls })
	if len(calls) != 1 || calls[0].From != SymbolID("sample.go", "Box.Run") || calls[0].To != SymbolID("sample.go", "Work") || calls[0].Line != 3 || calls[0].Confidence != Exact || calls[0].ID == "" || calls[0].Location.Path != "sample.go" {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestSharedSourceKeepsConsumerImportsAndFiltersResolverDiagnostics(t *testing.T) {
	source := []byte("from .missing import work\ndef entry():\n    work()\n")
	extractor, err := shared.NewExtractor(shared.ExtractionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := extractor.Extract(context.Background(), shared.Document{Path: "pkg/app.py", Content: source})
	if err != nil {
		t.Fatal(err)
	}
	if len(facts.Imports) != 1 || facts.Imports[0].From != "missing" || !reflect.DeepEqual(facts.Imports[0].Names, []string{"work"}) {
		t.Fatalf("imports = %+v", facts.Imports)
	}
	for _, issue := range facts.Issues {
		if issue.Code == "unresolved_import" || issue.Code == "unresolved_call" {
			t.Fatalf("single-file resolver diagnostic leaked: %+v", facts.Issues)
		}
	}
}

func TestSharedSourceKeepsScopedCallConfidence(t *testing.T) {
	source := []byte("def work():\n    pass\ndef work():\n    pass\ndef entry():\n    work()\n")
	sharedFacts := sourceFacts(t, "app.py", source)
	if len(sharedFacts.dependencies) == 0 || len(sharedFacts.Diagnostics) != 0 {
		t.Fatalf("shared facts = %+v", sharedFacts)
	}
	for _, call := range sharedFacts.dependencies {
		if call.Confidence != Scoped {
			t.Fatalf("candidate became exact: %+v", call)
		}
	}
}

func sourceFacts(t *testing.T, name string, data []byte) sharedSourceResult {
	t.Helper()
	builder, err := NewBuilder(context.Background(), BuildOptions{Files: map[string][]byte{name: data},
		Kinds: []Kind{Contains, Calls}, MaxFiles: 10, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.Add(context.Background(), name); err != nil {
		t.Fatal(err)
	}
	return builder.sources[name]
}
