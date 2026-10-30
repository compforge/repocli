package repocli_test

import (
	"context"
	"slices"
	"testing"

	cg "github.com/compforge/codegraph"
	"github.com/compforge/repocli/toolkit/go"
)

// +case:id=captured_resource_impact,desc=A tracked HTML resource changes,expect=Capture and impact preserve its reference and return the consuming test
func TestCapturedResourceReferenceImpactAndGraph(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-q", "-b", "main")
	put(t, root, "package.json", `{"name":"resource-fixture","type":"module"}`)
	put(t, root, "src/page.html", "old")
	put(t, root, "src/view.ts", `export function render() { return new URL('./page.html', import.meta.url); }`)
	put(t, root, "tests/view.test.ts", `import {render} from '../src/view';`)
	put(t, root, "tests/other.test.ts", `export function other() { return 1; }`)
	git(t, root, "add", ".")
	git(t, root, "commit", "-qm", "fixture")
	put(t, root, "src/page.html", "new")
	r, err := repocli.AnalyzeImpact(context.Background(), repocli.DiffRequest{Repository: root, TestDirs: []string{"tests"}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Scope != "focused" || !slices.Equal(r.TestFiles, []string{"tests/view.test.ts"}) {
		t.Fatalf("unexpected impact: %+v", r)
	}
	g, err := repocli.Graph(context.Background(), repocli.InputRequest{Repository: root}, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(g.Relations, func(r cg.Relation) bool { return r.Kind == cg.References && r.Target == cg.DocumentID("src/page.html") }) {
		t.Fatal("viewer graph lost resource reference")
	}
}
