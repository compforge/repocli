package analysis

import (
	"context"
	"reflect"
	"testing"

	cg "github.com/compforge/codegraph"
)

func TestPathTagsAgreeWithCodeGraph(t *testing.T) {
	for _, rules := range [][]cg.TagRule{nil, {}, {
		{Name: "custom", Pattern: `^fixtures$`},
		{Name: cg.GeneratedTag, Pattern: `\.pb\.go$`},
		{Name: cg.GeneratedTag, Pattern: `vendor`},
		{Name: "custom", Pattern: `\.json$`},
	}} {
		tags, err := compilePathTags(rules)
		if err != nil {
			t.Fatal(err)
		}
		paths := []string{"go.mod", "vendor/lib/a.pb.go", "fixtures/data.json", "dist/a.min.js", "kitex_gen/a.go", ".cache/data.txt", "plain.txt"}
		var docs []cg.Document
		for _, p := range paths {
			docs = append(docs, cg.Document{Path: p})
		}
		g, _, err := cg.Build(context.Background(), "s", docs, cg.Options{TagRules: rules})
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range g.Nodes() {
			if n.Kind == cg.DocumentNodeKind || n.Kind == cg.DirectoryNodeKind {
				if got := tags.match(n.Path); !reflect.DeepEqual(got, n.Tags) {
					t.Fatalf("%s: %v != %v", n.Path, got, n.Tags)
				}
			}
		}
	}
}
