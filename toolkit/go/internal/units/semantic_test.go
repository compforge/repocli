package units

import (
	"context"
	cg "github.com/compforge/codegraph/pkg"
	"testing"
)

// +case:id=semantic_unit_evidence,expect=`Changed occurrences retain their operation as touched declaration dependencies, never BindsTo or occurrence endpoints`
func TestSemanticUnitEvidence(t *testing.T) {
	files := map[string]string{"app.go": "package app\nvar count int\ntype Model struct{}\nfunc handler(){}\nfunc register(f func()){}\nfunc entry(m Model){handler(); register(handler); count += 1; _ = count}\n"}
	g := graph(t, "new", files)
	links, err := relations(context.Background(), functionFragments(t, files), nil, g)
	if err != nil {
		t.Fatal(err)
	}
	touched := map[cg.RelationKind]bool{}
	for _, e := range links {
		source, _ := g.Node(e.Link.Source)
		target, _ := g.Node(e.Link.Target)
		if e.Link.Kind == cg.BindsTo || source.Kind.IsOccurrence() || target.Kind.IsOccurrence() {
			t.Fatalf("navigation leaked into Unit dependency: %+v", e)
		}
		if e.Link.Touched {
			touched[e.Link.Kind] = true
		}
	}
	for _, kind := range []cg.RelationKind{cg.Calls, cg.Reads, cg.Writes, cg.UsesType} {
		if !touched[kind] {
			t.Errorf("missing touched %s: %+v", kind, links)
		}
	}
}
