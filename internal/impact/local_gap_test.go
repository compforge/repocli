package impact

import (
	"context"
	shared "github.com/compforge/codegraph"
	"github.com/compforge/repocli/internal/codegraph"
	"github.com/compforge/repocli/internal/diff"
	"slices"
	"testing"
)

// +spec=`Real declaration gaps remain observations and widen only overlapping changed seeds`
func TestLocalDeclarationGapContract(t *testing.T) {
	diagnostic := codegraph.Diagnostic{Path: "agent.ts", Code: "outline_incomplete", Subject: shared.DeclarationsSubject,
		Location: shared.Location{Path: "agent.ts", Line: 10, EndLine: 20}, Outline: &shared.OutlineCoverage{OmittedNameConflict: 1}}
	source := codegraph.Source{Language: "typescript", Symbols: []codegraph.Symbol{{QualifiedName: "work", StartLine: 1, EndLine: 30}}, Diagnostics: []codegraph.Diagnostic{diagnostic}}
	for _, line := range []int{5, 15} {
		change := diff.Change{Path: "agent.ts", Status: "modified", Hunks: []diff.Hunk{{Old: diff.Range{Start: line, Count: 1}, New: diff.Range{Start: line, Count: 1}}}}
		for _, before := range []bool{true, false} {
			seeds := selectSeeds(change, source, before)
			if line == 15 {
				if len(seeds) != 1 || seeds[0].Granularity != "file" || seeds[0].Basis != "declaration_gap" {
					t.Fatal(seeds)
				}
			} else {
				for _, seed := range seeds {
					if seed.Granularity == "file" {
						t.Fatal(seeds)
					}
				}
			}
		}
	}
	g := codegraph.New()
	g.AddRelation(codegraph.Relation{From: "tests/value.test.ts", To: "value.ts", Kind: codegraph.Imports})
	r := Result{Scope: "focused", Seeds: []Seed{{ID: "value.ts", Path: "value.ts", Version: "before"}, {ID: "value.ts", Path: "value.ts", Version: "after"}}}
	build := codegraph.BuildResult{Graph: g, Diagnostics: []codegraph.Diagnostic{diagnostic}}
	req := Request{After: map[string][]byte{"value.ts": {}, "tests/value.test.ts": {}}, TestDirs: []string{"tests"}}
	if err := r.selectTests(context.Background(), []*codegraph.Graph{g, g}, []codegraph.BuildResult{build, build}, req, []string{"tests/value.test.ts"}, nil); err != nil {
		t.Fatal(err)
	}
	if r.Scope != "focused" || len(r.FallbackReasons) != 0 || len(r.Observations) != 2 || !slices.Equal(r.TestFiles, []string{"tests/value.test.ts"}) {
		t.Fatalf("%+v", r)
	}
	for _, o := range r.Observations {
		if o.Path != diagnostic.Path || o.Location != diagnostic.Location || o.Outline.OmittedNameConflict != 1 {
			t.Fatal(o)
		}
	}
}
