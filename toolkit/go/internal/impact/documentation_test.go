package impact

import (
	"context"
	"slices"
	"testing"

	"github.com/compforge/repocli/toolkit/go/internal/diff"
)

// +spec=`Documentation classification preserves explicit asset dependencies`
func TestDocumentationImportStillSelectsConsumer(t *testing.T) {
	before := files(map[string]string{
		"docs/guide.md": "old docs",
		"guide.test.ts": "import guide from './docs/guide.md';",
	})
	after := files(map[string]string{
		"docs/guide.md": "new docs",
		"guide.test.ts": "import guide from './docs/guide.md';",
	})
	got, err := Analyze(context.Background(), Request{Before: before, After: after,
		Changes: []diff.Change{{Path: "docs/guide.md", Status: "modified"}}, TestDirs: []string{"."}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Scope != "focused" || !slices.Equal(got.TestFiles, []string{"guide.test.ts"}) {
		t.Fatalf("documentation consumer: %+v", got)
	}
	if len(got.Observations) != 1 || got.Observations[0].Reason != "documentation_change" {
		t.Fatalf("documentation observation: %+v", got.Observations)
	}
}
