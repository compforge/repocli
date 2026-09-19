package codegraph

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func scopeFixture() map[string][]byte {
	source := map[string]string{
		"go.mod":               "module example.com/app\n\ngo 1.25.0\n",
		"a/a.go":               "package a\nfunc Value() int { return 1 }\n",
		"a/a_test.go":          "package a\nvar _ = Value\n",
		"b/b.go":               "package b\nfunc Value() int { return 2 }\n",
		"b/b_test.go":          "package b\nvar _ = Value\n",
		"bridge/bridge.go":     "package bridge\nimport \"example.com/app/a\"\nvar Value = a.Value\n",
		"consumer/use.go":      "package consumer\nimport \"example.com/app/bridge\"\nvar Value = bridge.Value\n",
		"consumer/use_test.go": "package consumer\nvar _ = Value\n",
		"external/use_test.go": "package external_test\nimport \"example.com/app/a\"\nvar _ = a.Value\n",
		"other.test.ts":        "export const unrelated = 1;",
	}
	files := map[string][]byte{}
	for name, data := range source {
		files[name] = []byte(data)
	}
	return files
}

func scopeBuilder(t testing.TB, files map[string][]byte) *Builder {
	t.Helper()
	builder, err := NewBuilder(BuildOptions{Files: files, Kinds: []Kind{Imports, PackageMember}, SymbolFiles: []string{}, MaxDepth: 32, MaxFiles: 2000})
	if err != nil {
		t.Fatal(err)
	}
	return builder
}

func TestScopeCandidatesKeepsGoConsumersWithoutBuildingGraph(t *testing.T) {
	candidates := []string{"a/a_test.go", "b/b_test.go", "consumer/use_test.go", "external/use_test.go", "other.test.ts"}
	builder := scopeBuilder(t, scopeFixture())
	got, err := builder.ScopeCandidates(context.Background(), []string{"a/a.go"}, candidates)
	want := []string{"a/a_test.go", "consumer/use_test.go", "external/use_test.go", "other.test.ts"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("%v %v", got, err)
	}
	if result := builder.Result(); len(result.Graph.Nodes) != 0 || len(result.ParsedFiles) != 0 {
		t.Fatal("scoping expanded the graph")
	}
	for _, name := range got {
		if err := builder.Add(context.Background(), name); err != nil {
			t.Fatal(err)
		}
	}
	result := builder.Result()
	for _, name := range result.ParsedFiles {
		if strings.HasPrefix(name, "b/") {
			t.Fatalf("parsed unrelated file %s", name)
		}
	}
	query := result.Query([]string{"a/a.go"}, candidates, []Kind{Imports, PackageMember})
	for _, name := range want[:3] {
		if _, ok := query.Paths[name]; !ok {
			t.Errorf("missing evidence for %s", name)
		}
	}
	if _, ok := query.Paths["b/b_test.go"]; ok {
		t.Fatal("invented unrelated relationship")
	}
}

func TestScopeCandidatesRetainsUncertainInputs(t *testing.T) {
	cases := []struct{ name, data string }{
		{"go.mod", "invalid module contents"},
		{"go.mod", "module example.com/app\nreplace example.com/lib => ../lib\n"},
		{"b/b.go", "package b\nimport ("},
		{"b/b.go", "package b\nimport \"example.com/app/missing\"\n"},
		{"b/b.go", "package b\nimport \"../a\"\n"},
	}
	candidates := []string{"a/a_test.go", "b/b_test.go"}
	for _, tc := range cases {
		files := scopeFixture()
		files[tc.name] = []byte(tc.data)
		got, err := scopeBuilder(t, files).ScopeCandidates(context.Background(), []string{"a/a.go"}, candidates)
		if err != nil || !reflect.DeepEqual(got, candidates) {
			t.Fatalf("%s: %v %v", tc.data, got, err)
		}
	}
	boundary := scopeFixture()
	boundary["vendor/hidden/source.go"] = []byte("package hidden\nimport \"example.com/app/a\"\n")
	boundary["b/b.go"] = []byte("package b\nimport \"example.com/app/vendor/hidden\"\n")
	if got, err := scopeBuilder(t, boundary).ScopeCandidates(context.Background(), []string{"a/a.go"}, candidates); err != nil || !reflect.DeepEqual(got, candidates) {
		t.Fatalf("pruned across unavailable package: %v %v", got, err)
	}
	for _, entries := range [][]string{nil, {"go.mod"}, {"a/a.go", "resource.json"}} {
		got, err := scopeBuilder(t, scopeFixture()).ScopeCandidates(context.Background(), entries, candidates)
		if err != nil || !reflect.DeepEqual(got, candidates) {
			t.Fatalf("%v: %v %v", entries, got, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := scopeBuilder(t, scopeFixture()).ScopeCandidates(ctx, []string{"a/a.go"}, candidates); err != context.Canceled {
		t.Fatal(err)
	}
}

func TestScopeCandidatesPreservesBuildTaggedImportsAndCycles(t *testing.T) {
	files := scopeFixture()
	files["b/tagged.go"] = []byte("//go:build custom\n\npackage b\nimport \"example.com/app/a\"\nvar _ = a.Value\n")
	files["a/cycle.go"] = []byte("package a\nimport \"example.com/app/b\"\nvar _ = b.Value\n")
	candidates := []string{"a/a_test.go", "b/b_test.go"}
	got, err := scopeBuilder(t, files).ScopeCandidates(context.Background(), []string{"a/a.go"}, candidates)
	if err != nil || !reflect.DeepEqual(got, candidates) {
		t.Fatalf("%v %v", got, err)
	}
}

func BenchmarkGoCandidateScope(b *testing.B) {
	files := map[string][]byte{"go.mod": []byte("module example.com/app\n\ngo 1.25.0\n")}
	var candidates []string
	for i := range 80 {
		dir := fmt.Sprintf("p%03d", i)
		body := "package " + dir + "\n"
		for j := range 30 {
			body += fmt.Sprintf("func F%d() int { return %d }\n", j, j)
		}
		files[dir+"/source.go"] = []byte(body)
		files[dir+"/source_test.go"] = []byte("package " + dir + "\nvar _ = F0\n")
		candidates = append(candidates, dir+"/source_test.go")
	}
	for _, scoped := range []bool{false, true} {
		b.Run(fmt.Sprintf("scoped=%t", scoped), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				builder := scopeBuilder(b, files)
				selected := candidates
				if scoped {
					var err error
					selected, err = builder.ScopeCandidates(context.Background(), []string{"p000/source.go"}, candidates)
					if err != nil {
						b.Fatal(err)
					}
				}
				for _, name := range selected {
					if err := builder.Add(context.Background(), name); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(len(builder.Result().ParsedFiles)), "parsed/op")
			}
		})
	}
}
