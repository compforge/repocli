package codegraph

import (
	"context"
	"slices"
	"testing"
)

func TestSourceScopePreservesReferencesWithoutParsingTargets(t *testing.T) {
	for _, tc := range []struct {
		name           string
		files          map[string][]byte
		root, excluded string
	}{
		{"typescript", map[string][]byte{"a.ts": []byte("import {value} from './b';"), "b.ts": []byte("export const value=1;")}, "a.ts", "b.ts"},
		{"go", map[string][]byte{"a/go.mod": []byte("module example.com/a\n"), "b/go.mod": []byte("module example.com/b\n"), "a/a.go": []byte("package a\nimport \"example.com/b\"\nvar _ = b.Value\n"), "b/b.go": []byte("package b\nfunc Value() {}\n")}, "a/a.go", "b/b.go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := NewBuilder(context.Background(), BuildOptions{Files: tc.files, SourceFiles: map[string]bool{tc.root: true}, Kinds: []Kind{Imports, PackageMember}, MaxFiles: 1, MaxDepth: 32})
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := b.Add(context.Background(), tc.root, tc.excluded); err != nil {
					t.Fatal(err)
				}
			}
			got := b.Result()
			if !slices.Equal(got.ParsedFiles, []string{tc.root}) || len(got.Graph.Outgoing(tc.root)) == 0 {
				t.Fatal(got)
			}
			if len(got.Diagnostics) != 1 || got.Diagnostics[0].Code != "source_scope" || got.Diagnostics[0].Path != tc.excluded {
				t.Fatal(got.Diagnostics)
			}
		})
	}
}
