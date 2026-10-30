package impact

import (
	"context"
	"slices"
	"testing"

	"github.com/compforge/repocli/toolkit/go/internal/codegraph"
	"github.com/compforge/repocli/toolkit/go/internal/diff"
)

// +case:id=resource_reference_impact,desc=An asset changes without changing its consumers,expect=Only tests connected by static references are selected
func TestResourceReferencesSelectDependentTests(t *testing.T) {
	for _, source := range []string{
		`export function render() { return new URL('./template.html', import.meta.url); }`,
		`import {URL as Asset} from 'node:url'; export function render() { return new Asset('./template.html', import.meta.url); }`,
		`export function render() { const asset = new URL('./template.html', import.meta.url); return asset; }`,
	} {
		t.Run(source, func(t *testing.T) {
			base := map[string]string{
				"src/template.html":   "old",
				"src/view.ts":         source + ` export function unrelated() { return 1; }`,
				"tests/view.test.ts":  `import {render} from '../src/view';`,
				"tests/other.test.ts": `import {unrelated} from '../src/other';`,
				"src/other.ts":        `export function unrelated() { return 1; }`,
			}
			r := runChange(t, base, "src/template.html", "old", "new")
			if !slices.Equal(r.TestFiles, []string{"tests/view.test.ts"}) || r.Scope != "focused" {
				t.Fatalf("unexpected impact: %+v", r)
			}
			var evidenced bool
			for _, reason := range r.Reasons {
				for _, edge := range reason.Relations {
					if edge.Kind == codegraph.References && edge.To == "src/template.html" {
						evidenced = len(edge.Evidence) > 0 && edge.ID != "" && edge.Location.Line > 0
					}
				}
			}
			if !evidenced {
				t.Fatal("missing native reference evidence", r.Reasons)
			}
		})
	}
}

// +spec=Resource references use snapshot-specific file identities for deletions and renames.
func TestResourceReferenceRemovedOrRenamed(t *testing.T) {
	for _, rename := range []bool{false, true} {
		before := files(map[string]string{
			"src/template.html":  "old",
			"src/view.ts":        `export function render() { return new URL('./template.html', import.meta.url); }`,
			"tests/view.test.ts": `import {render} from '../src/view';`,
		})
		after := files(map[string]string{
			"src/view.ts":        `export function render() { return 'gone'; }`,
			"tests/view.test.ts": string(before["tests/view.test.ts"]),
		})
		change := diff.Change{Path: "src/template.html", Status: "deleted"}
		if rename {
			change = diff.Change{Path: "src/renamed.html", OldPath: "src/template.html", Status: "renamed"}
			after["src/renamed.html"] = []byte("old")
		}
		r, err := Analyze(context.Background(), Request{Before: before, After: after, TestDirs: []string{"tests"}, Changes: []diff.Change{change}})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(r.TestFiles, []string{"tests/view.test.ts"}) || r.Reasons[0].Version != "before" {
			t.Fatalf("lost old reference: %+v", r)
		}
	}
}

// +spec=Strings and shadowed constructors never manufacture a dependency; dynamic paths stay explicit gaps.
func TestUnprovenResourceReferences(t *testing.T) {
	for _, source := range []string{
		`export function render() { return './template.html'; }`,
		`export function render(URL) { return new URL('./template.html', import.meta.url); }`,
		"export function render(name) { return new URL(`./${name}.html`, import.meta.url); }",
	} {
		base := map[string]string{
			"src/template.html": "old", "src/view.ts": source,
			"tests/view.test.ts": `import {render} from '../src/view';`,
		}
		r := runChange(t, base, "src/template.html", "old", "new")
		if len(r.TestFiles) != 0 || r.Scope != "partial" {
			t.Fatalf("invented reference: %+v", r)
		}
		if source == "export function render(name) { return new URL(`./${name}.html`, import.meta.url); }" &&
			!slices.ContainsFunc(r.Observations, func(u Uncertainty) bool { return u.Reason == "unresolved_resource" }) {
			t.Fatalf("missing dynamic path diagnostic: %+v", r)
		}
	}
}
