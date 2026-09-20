package codegraph

import (
	"context"
	"reflect"
	"testing"

	"github.com/compforge/repocli/internal/codegraph/internal/syntax"
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
	source := new(Analyzer).Source(context.Background(), "Main.java", []byte("class Main { void run() {} }"))
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
	facts := sharedSource(context.Background(), "sample.go", source)
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
	if !reflect.DeepEqual(facts.calls, []syntax.LocalCall{{Caller: "Box.Run", Callee: "Work", Line: 3}}) {
		t.Fatalf("calls = %+v", facts.calls)
	}
}

func TestSharedSourceKeepsConsumerImportsAndFiltersResolverDiagnostics(t *testing.T) {
	source := []byte("from .missing import work\ndef entry():\n    work()\n")
	analyzer := &Analyzer{}
	facts := analyzer.AnalyzeFeatures(context.Background(), "pkg/app.py", source, syntax.Features{Symbols: true, Calls: true})
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
	sharedFacts := sharedSource(context.Background(), "app.py", source)
	if len(sharedFacts.calls) != 0 || len(sharedFacts.Issues) != 0 {
		t.Fatalf("shared facts = %+v", sharedFacts)
	}
	facts := new(Analyzer).AnalyzeFeatures(context.Background(), "app.py", source, syntax.Features{Symbols: true, Calls: true})
	if len(facts.Calls) != 0 || len(facts.Issues) != 1 || facts.Issues[0].Code != "binding_ambiguous" {
		t.Fatalf("issues = %+v", facts.Issues)
	}
}
