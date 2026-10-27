package impact

import (
	"context"
	"slices"
	"testing"

	"github.com/compforge/repocli/toolkit/go/internal/diff"
)

// +case=documentation_dependency_and_gap
// +spec=Known documentation consumers remain selected alongside unmodeled resource impact.
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
	if got.Scope != "partial" || !slices.Equal(got.TestFiles, []string{"guide.test.ts"}) {
		t.Fatalf("documentation consumer: %+v", got)
	}
	if len(got.Uncertainties) != 1 || got.Uncertainties[0].Reason != "unsupported_resource_change" || got.Uncertainties[0].Path != "docs/guide.md" {
		t.Fatalf("resource gap: %+v", got.Uncertainties)
	}
}
