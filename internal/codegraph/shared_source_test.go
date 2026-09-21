package codegraph

import (
	"context"
	"reflect"
	"testing"
)

func TestLanguageRequiresDeclarationCapability(t *testing.T) {
	for file, want := range map[string]string{
		"main.go": "go", "Main.java": "java", "main.rs": "rust",
		"data.json": "", "README.md": "", "unknown.repocli": "",
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
	if len(source.Issues) != 1 || source.Issues[0].Code != "unsupported_resolution" {
		t.Fatalf("issues = %+v", source.Issues)
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
	if !reflect.DeepEqual(facts.calls, []localCall{{caller: "Box.Run", callee: "Work", line: 3}}) {
		t.Fatalf("calls = %+v", facts.calls)
	}
}

func TestSharedSourceKeepsConsumerImportsAndFiltersResolverDiagnostics(t *testing.T) {
	source := []byte("from .missing import work\ndef entry():\n    work()\n")
	analyzer := &Analyzer{}
	facts := analyzer.Analyze(context.Background(), "pkg/app.py", source)
	if len(facts.Imports) != 1 || facts.Imports[0].From != "missing" || !reflect.DeepEqual(facts.Imports[0].Names, []string{"work"}) {
		t.Fatalf("imports = %+v", facts.Imports)
	}
	for _, issue := range facts.Issues {
		if issue.Code == "unresolved_import" || issue.Code == "unresolved_call" {
			t.Fatalf("single-file resolver diagnostic leaked: %+v", facts.Issues)
		}
	}
}

func TestSharedSourceDoesNotPromoteCandidateCalls(t *testing.T) {
	source := []byte("def work():\n    pass\ndef work():\n    pass\ndef entry():\n    work()\n")
	sharedFacts := sourceFacts(t, "app.py", source)
	if len(sharedFacts.calls) != 0 || len(sharedFacts.Issues) != 1 || sharedFacts.Issues[0].Code != "ambiguous_call" {
		t.Fatalf("shared facts = %+v", sharedFacts)
	}
}

func sourceFacts(t *testing.T, name string, data []byte) sharedSourceResult {
	t.Helper()
	builder, err := NewBuilder(BuildOptions{Files: map[string][]byte{name: data},
		Kinds: []Kind{Contains, Calls}, MaxFiles: 10, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.Add(context.Background(), name); err != nil {
		t.Fatal(err)
	}
	return builder.sources[name]
}
