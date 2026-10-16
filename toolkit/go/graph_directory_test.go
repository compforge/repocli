package repocli_test

import (
	"context"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	cg "github.com/compforge/codegraph"
	"github.com/compforge/repocli/toolkit/go"
)

// +case:id=graph_directory_pruning,desc=`Excluded or out-of-scope subtrees contain unavailable ignore rules`,expect=`Only admitted directories load rules; omitted material consumes no document budget`
func TestGraphDirectoryPruningPrecedesIgnoreReads(t *testing.T) {
	for _, tc := range []struct {
		name, rule, ignored string
		selectPath          string
		excluded            []cg.Tag
		tagRules            []cg.TagRule
	}{
		{name: "tooling tag", ignored: ".codex", excluded: []cg.Tag{cg.ToolingTag}},
		{name: "dependency tag", ignored: "node_modules", excluded: []cg.Tag{cg.DependencyTag}},
		{name: "fixture tag", ignored: "testdata", excluded: []cg.Tag{cg.TestFixtureTag}},
		{name: "replacement directory tag", ignored: "blocked", excluded: []cg.Tag{cg.GeneratedTag}, tagRules: []cg.TagRule{{Name: cg.GeneratedTag, Pattern: `^blocked$`}}},
		{name: "parent ignore", rule: "blocked/\n", ignored: "blocked"},
		{name: "nested parent ignore", rule: "blocked/\n", ignored: "nested/blocked", selectPath: "nested"},
		{name: "scope selection", ignored: "outside", selectPath: "main.go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{"main.go": "package p\nfunc Main(){}\n", tc.ignored + "/bad.go": "invalid source"}
			if tc.selectPath == "nested" {
				files["nested/main.go"] = files["main.go"]
				delete(files, "main.go")
			}
			if tc.rule != "" {
				name := ".gitignore"
				if tc.selectPath == "nested" {
					name = "nested/.gitignore"
				}
				files[name] = tc.rule
			}
			source := graphMemory(files)
			unavailable := tc.ignored + "/.gitignore"
			source.catalog = append(source.catalog, repocli.SourceFile{Path: unavailable, Size: -1})
			opts := sourceGraphOptions()
			opts.CodeGraph.MaxDocuments = 1
			opts.CodeGraph.TagRules = tc.tagRules
			opts.CodeGraph.Admission = &cg.AdmissionPolicy{ExcludedTags: tc.excluded}
			if tc.name == "scope selection" {
				opts.Select = func(name string, _ []cg.Tag) bool { return name == tc.selectPath }
			}
			got, err := repocli.GraphFromSource(context.Background(), source, opts)
			if err != nil {
				t.Fatal(err)
			}
			if got.Documents != 1 || len(got.InputDiagnostics) != 0 {
				t.Fatalf("unexpected coverage: %+v", got)
			}
			for name, reads := range source.reads {
				if reads != 0 && strings.HasPrefix(name, tc.ignored+"/") {
					t.Fatalf("excluded material read: %s", name)
				}
			}
			if source.reads[unavailable] != 0 {
				t.Fatal("unavailable rule read")
			}
		})
	}
}

func TestGraphDirectoryRulesPreserveNegationAndReadOnce(t *testing.T) {
	source := graphMemory(map[string]string{
		".gitignore":           "tree/*\n!tree/keep/\n",
		"tree/keep/.gitignore": "*.go\n!keep.go\n",
		"tree/keep/keep.go":    "package p\nfunc Keep(){}\n",
		"tree/keep/drop.go":    "invalid source",
		"tree/drop/bad.go":     "invalid source",
	})
	source.catalog = append(source.catalog, repocli.SourceFile{Path: "tree/drop/.gitignore", Size: -1})
	opts := sourceGraphOptions()
	opts.Select = nil
	got, err := repocli.GraphFromSource(context.Background(), source, opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Sources["tree/keep/keep.go"]; !ok {
		t.Fatal("negated descendant omitted")
	}
	for _, name := range []string{"tree/drop/.gitignore", "tree/drop/bad.go", "tree/keep/drop.go"} {
		if source.reads[name] != 0 {
			t.Fatalf("ignored material read: %s", name)
		}
		if _, ok := got.Sources[name]; ok {
			t.Fatalf("ignored material admitted: %s", name)
		}
	}
	for _, name := range []string{".gitignore", "tree/keep/.gitignore"} {
		if source.reads[name] != 1 {
			t.Fatalf("rule %s read %d times", name, source.reads[name])
		}
	}
}

func TestGraphRequiredIgnoreRulesFailExplicitly(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int64
		want string
	}{
		{"unavailable", -1, "exceeds byte limit"},
		{"oversized", 4097, "exceeds byte limit"},
		{"unreadable", 1, "read snapshot ignore file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := graphMemory(map[string]string{"selected/main.go": "package p\nfunc Main(){}\n"})
			source.catalog = append(source.catalog, repocli.SourceFile{Path: ".gitignore", Size: tc.size})
			_, err := repocli.GraphFromSource(context.Background(), source, sourceGraphOptions())
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), ".gitignore") {
				t.Fatalf("missing rule failure: %v", err)
			}
			if source.reads["selected/main.go"] != 0 {
				t.Fatal("source loaded before required ignore rules")
			}
		})
	}
}

func TestGraphIgnoreMetadataHasAggregateByteLimit(t *testing.T) {
	source := graphMemory(map[string]string{".gitignore": "# root\n", "sub/.gitignore": "# nested\n", "sub/main.go": "package p\n"})
	opts := sourceGraphOptions()
	opts.CodeGraph.MaxSourceBytes = 10
	_, err := repocli.GraphFromSource(context.Background(), source, opts)
	if err == nil || !strings.Contains(err.Error(), "total byte limit") {
		t.Fatalf("unbounded ignore metadata: %v", err)
	}
	if source.reads["sub/.gitignore"] != 0 {
		t.Fatal("over-budget rule was read")
	}
}

// +case:id=graph_source_admission_parity,desc=`The same tracked files are observed through commit, index and directory sources`,expect=`Explicit graph admission selects the same material across input adapters`
func TestGraphSourceAdaptersShareDirectoryAdmission(t *testing.T) {
	root := fixture(t)
	for _, name := range []string{".domain/core.go", ".idea/core.go", "testdata/example.go", "custom_tools/core.go"} {
		put(t, root, name, "package p\nfunc Core(){}\n")
	}
	git(t, root, "add", ".")
	git(t, root, "commit", "-qm", "material catalog")
	for _, tagRules := range [][]cg.TagRule{{}, {{Name: cg.ToolingTag, Pattern: `^custom_tools$`}}} {
		var want []string
		for _, req := range []repocli.InputRequest{{Repository: root, Head: "HEAD"}, {Repository: root, Staged: true}, {Repository: root}} {
			source, err := repocli.OpenGraphSource(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			opts := sourceGraphOptions()
			opts.CodeGraph.MaxDocuments = 100
			opts.CodeGraph.TagRules = tagRules
			opts.CodeGraph.Admission = &cg.AdmissionPolicy{ExcludedTags: []cg.Tag{cg.ToolingTag}}
			got, err := repocli.GraphFromSource(context.Background(), source, opts)
			if err != nil {
				t.Fatal(err)
			}
			names := make([]string, 0, len(got.Sources))
			for name := range got.Sources {
				names = append(names, name)
			}
			sort.Strings(names)
			if want == nil {
				want = names
			} else if !reflect.DeepEqual(names, want) {
				t.Fatalf("input %+v: %v, want %v", req, names, want)
			}
			for _, name := range []string{".domain/core.go", ".idea/core.go", "testdata/example.go"} {
				if !slices.Contains(names, name) {
					t.Fatalf("implicit source exclusion: %s", name)
				}
			}
			if slices.Contains(names, "custom_tools/core.go") != (len(tagRules) == 0) {
				t.Fatal("replacement tag rules ignored")
			}
		}
	}
}
