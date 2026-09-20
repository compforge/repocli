package syntax

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

func TestDeclarationCoverage(t *testing.T) {
	cases := []struct {
		name, source string
		want         map[string]string
	}{
		{"decl.go", `package demo
 type Item struct{}
 type Alias = Item
 type (
  First int
  Second string
 )
 const (
  Limit = 10
  Label = "name"
 )
 var Current Item
 var _ Item
 const _ = 0
 func (i *Item) Work() {}
 func Run() {}
`, map[string]string{"Item": "type", "Alias": "type", "First": "type", "Second": "type", "Limit": "constant", "Label": "constant", "Current": "variable", "Item.Work": "method", "Run": "function"}},
		{"decl.py", "class Item:\n    def work(self): pass\ndef run():\n    def nested(): pass\n", map[string]string{"Item": "class", "Item.work": "function", "run": "function", "run.nested": "function"}},
		{"decl.js", "class Item { work() {} }\nfunction run() {}\nconst value = 1;\nlet View = () => value;\n", map[string]string{"Item": "class", "Item.work": "method", "run": "function", "value": "variable", "View": "variable"}},
		{"decl.ts", "interface Item { value: string }\ntype Alias = Item;\nenum State { Ready }\nconst value = 1;\n", map[string]string{"Item": "interface", "Alias": "type", "State": "enum", "value": "variable"}},
		{"decl.tsx", "interface Props { value: string }\ntype Alias = Props;\nconst View = () => <div/>;\n", map[string]string{"Props": "interface", "Alias": "type", "View": "variable"}},
	}
	a := &Analyzer{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := a.AnalyzeFeatures(context.Background(), tc.name, []byte(tc.source), Features{Symbols: true})
			if len(f.Issues) != 0 {
				t.Fatalf("issues: %+v", f.Issues)
			}
			got := map[string]string{}
			for _, s := range f.Symbols {
				got[s.QualifiedName] = s.Kind
				if s.StartByte >= s.EndByte || s.EndByte > uint32(len(tc.source)) || s.StartLine < 1 || s.EndLine < s.StartLine {
					t.Fatalf("invalid declaration span: %+v", s)
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSharedDeclarationSpanReportsAmbiguity(t *testing.T) {
	for _, source := range []string{"package demo\nvar A, B = 1, 2\n", "package demo\nconst A, B = 1, 2\n"} {
		f := (&Analyzer{}).Analyze(context.Background(), "decl.go", []byte(source))
		found := false
		for _, issue := range f.Issues {
			if issue.Code == "outline_incomplete" {
				found = true
			}
		}
		if !found || len(f.Symbols) != 0 {
			t.Fatalf("ambiguous declaration: symbols=%+v issues=%+v", f.Symbols, f.Issues)
		}
	}
}

func TestAnalyzerFeatureOrderPreservesFacts(t *testing.T) {
	source := []byte("from pkg import value as alias\ndef helper(): return alias\ndef api(): return helper()\n")
	for _, order := range [][]Features{
		{{}, {Symbols: true}, {Symbols: true, Calls: true}, {}},
		{{Calls: true}, {}, {Symbols: true}},
	} {
		a := &Analyzer{}
		for i, features := range order {
			// Alternate paths to force source extraction as well as reuse of compiled
			// programs. Compare with a cold analyzer to guard shallow/detailed isolation.
			name := fmt.Sprintf("file%d.py", i%2)
			got := a.AnalyzeFeatures(context.Background(), name, source, features)
			want := (&Analyzer{}).AnalyzeFeatures(context.Background(), name, source, features)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("features=%+v: got %+v, want %+v", features, got, want)
			}
			if len(got.Imports) != 1 || len(got.Issues) != 0 {
				t.Fatalf("lost import facts: %+v", got)
			}
			if features.Calls && (len(got.Calls) != 1 || got.Calls[0].Caller != "api" || got.Calls[0].Callee != "helper") {
				t.Fatalf("lost call facts: %+v", got)
			}
		}
	}
}

func BenchmarkSyntaxExtractorReuse(b *testing.B) {
	source := []byte("from pkg import value\ndef helper(): return value\ndef api(): return helper()\n")
	for _, reuse := range []bool{false, true} {
		b.Run(fmt.Sprintf("reuse=%t", reuse), func(b *testing.B) {
			a := &Analyzer{}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if !reuse {
					a = &Analyzer{}
				}
				// Measure extraction, not identical-content result caching.
				a.cache = nil
				f := a.Analyze(context.Background(), "file.py", source)
				if len(f.Calls) != 1 || len(f.Issues) != 0 {
					b.Fatalf("unexpected facts: %+v", f)
				}
			}
		})
	}
}
