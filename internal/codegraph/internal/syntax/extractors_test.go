package syntax

import (
	"context"
	"reflect"
	"testing"
)

func TestAnalyzerReusesOnlyRepositoryFacts(t *testing.T) {
	source := []byte("from pkg import value as alias\ndef api(): return alias\n")
	a := &Analyzer{}
	for _, name := range []string{"first.py", "second.py"} {
		got := a.Analyze(context.Background(), name, source)
		want := (&Analyzer{}).Analyze(context.Background(), name, source)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("name=%s: got %+v, want %+v", name, got, want)
		}
		if len(got.Imports) != 1 || len(got.Issues) != 0 {
			t.Fatalf("lost repository facts: %+v", got)
		}
	}
}

func BenchmarkSyntaxExtractorReuse(b *testing.B) {
	source := []byte("from pkg import value\ndef helper(): return value\ndef api(): return helper()\n")
	for _, reuse := range []bool{false, true} {
		b.Run("reuse="+map[bool]string{false: "false", true: "true"}[reuse], func(b *testing.B) {
			a := &Analyzer{}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if !reuse {
					a = &Analyzer{}
				}
				a.cache = nil
				facts := a.Analyze(context.Background(), "file.py", source)
				if len(facts.Imports) != 1 || len(facts.Issues) != 0 {
					b.Fatalf("unexpected facts: %+v", facts)
				}
			}
		})
	}
}
