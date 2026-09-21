package syntax

import (
	"context"
	"slices"
	"testing"
)

func TestLanguageFacts(t *testing.T) {
	cases := []struct {
		name, source, dependency string
		names                    []string
	}{
		{"source.go", "package sample\nimport \"fmt\"\nfunc A() { fmt.Println(1) }\n", "fmt", nil},
		{"source.py", "from pkg import a as alias\ndef work():\n    return alias()\n", "pkg.a", []string{"a"}},
		{"source.ts", "import { a as alias, b } from './dep';\nexport function work() { return alias(); }\n", "./dep", []string{"a", "b"}},
		{"source.tsx", "import * as dep from './dep';\nexport const View = () => <div>{dep.a()}</div>;\n", "./dep", nil},
		{"source.js", "const dep = require('./dep');\nfunction work() { return dep.a(); }\n", "./dep", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &Analyzer{}
			f := a.Analyze(context.Background(), tc.name, []byte(tc.source))
			if len(f.Issues) > 0 {
				t.Fatalf("issues: %v", f.Issues)
			}
			found := false
			for _, i := range f.Imports {
				if i.Path == tc.dependency && slices.Equal(i.Names, tc.names) {
					found = true
				}
			}
			if !found {
				t.Fatalf("unexpected imports: %+v", f.Imports)
			}
		})
	}
}

func TestDynamicImportsAndParseErrorsAreVisible(t *testing.T) {
	for _, source := range []string{"import(moduleName);", "export function broken( {", "require('./' + name);"} {
		f := (&Analyzer{}).Analyze(context.Background(), "file.ts", []byte(source))
		if len(f.Issues) == 0 {
			t.Errorf("no issue for %q", source)
		}
	}
}

func TestExportAlias(t *testing.T) {
	a := &Analyzer{}
	f := a.Analyze(context.Background(), "a.ts", []byte("function local() {}\nexport { local as publicName };\n"))
	if f.Exports["publicName"] != "local" {
		t.Fatalf("exports: %+v", f.Exports)
	}
}
