package syntax

import (
	"context"
	"slices"
	"testing"
)

func TestQualifiedOwners(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		want         []string
	}{
		{"a.py", "class A:\n    def work(self): pass\nclass B:\n    def work(self): pass\ndef outer():\n    def inner(): pass\n", []string{"A.work", "B.work", "outer.inner"}},
		{"a.ts", "class A { work() {} }\nclass B { work() {} }\nconst View = () => 1;\ntype Value = string;", []string{"A.work", "B.work", "View", "Value"}},
		{"a.go", "package demo\ntype A struct{}\ntype B struct{}\nfunc (a A) Work() {}\nfunc (b *B) Work() {}\n", []string{"A.Work", "B.Work"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := (&Analyzer{}).Analyze(context.Background(), tc.name, []byte(tc.source))
			var got []string
			for _, s := range f.Symbols {
				got = append(got, s.QualifiedName)
			}
			for _, name := range tc.want {
				if !slices.Contains(got, name) {
					t.Fatalf("missing %s: %+v issues=%v", name, f.Symbols, f.Issues)
				}
			}
		})
	}
}

func TestLocalCallsResolveOnlyUnshadowedBindings(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		calls        int
		issue        bool
	}{
		{"a.py", "def helper(): return 1\ndef api(): return helper()\n", 1, false},
		{"a.ts", "function helper() { return 1; }\nexport function api() { return helper(); }", 1, false},
		{"parameter.py", "def helper(): return 1\ndef api(helper): return helper()\n", 0, true},
		{"parameter.ts", "function helper() {}\nfunction api(helper: () => void) { helper(); }", 0, true},
		{"assignment.py", "def helper(): return 1\ndef api():\n    helper = other\n    return helper()\n", 0, true},
		{"callback.ts", "function helper() {}\nfunction api() { return items.map(helper => helper()); }", 0, true},
		{"unrelated.py", "def helper(): return 1\ndef other(helper): return 0\ndef api(): return helper()\n", 1, false},
		{"nested.py", "def helper(): return 1\ndef api():\n    def helper(): return 2\n    return helper()\n", 0, true},
		{"method.ts", "function helper() {}\nclass A { helper() {} work() { this.helper(); } }", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := (&Analyzer{}).Analyze(context.Background(), tc.name, []byte(tc.source))
			if len(f.Calls) != tc.calls || (len(f.Issues) > 0) != tc.issue {
				t.Fatalf("calls=%+v issues=%v", f.Calls, f.Issues)
			}
			if tc.calls == 1 && (f.Calls[0].Caller != "api" || f.Calls[0].Callee != "helper" || f.Calls[0].Line == 0) {
				t.Fatal(f.Calls)
			}
		})
	}
}
