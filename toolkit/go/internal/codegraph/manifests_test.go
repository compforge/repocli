package codegraph

import (
	"context"
	"reflect"
	"slices"
	"testing"

	shared "github.com/compforge/codegraph"
)

func TestManifestContextKeepsVersionAndConsumerConfig(t *testing.T) {
	for _, module := range []string{"example.com/before", "example.com/after"} {
		files := map[string][]byte{
			"go.mod":         []byte("module " + module + "\nreplace example.com/dep => ./local\n"),
			"nested/go.mod":  []byte("module example.com/nested\n"),
			"main.go":        []byte("package main\nfunc main() {}\n"),
			"package.json":   []byte(`{"name":"@example/work","exports":"./main.ts","packageManager":"pnpm@10"}`),
			"pyproject.toml": []byte("[project]\nname = 'example-work'\nversion = '1.0'\n"),
		}
		graph, err := BuildManifests(context.Background(), files)
		if err != nil {
			t.Fatal(err)
		}
		if got := GoModules(graph); !reflect.DeepEqual(got, map[string]string{".": module, "nested": "example.com/nested"}) {
			t.Fatalf("modules: %+v", got)
		}
		for _, name := range []string{"go.mod", "package.json", "pyproject.toml"} {
			document, ok := graph.Document(name)
			if !ok || !slices.Contains(document.Tags, shared.ManifestTag) || document.Manifest == nil {
				t.Fatalf("document %s: %+v", name, document)
			}
			if Language(name) != "" {
				t.Fatalf("manifest became source: %s", name)
			}
		}
		b, err := NewBuilder(context.Background(), BuildOptions{Files: files, Manifests: graph, MaxFiles: 1, MaxDepth: 1})
		if err != nil {
			t.Fatal(err)
		}
		if b.resolver.modules["."] != module || b.resolver.packages["."].Name != "@example/work" || string(b.resolver.packages["."].Exports) != `"./main.ts"` {
			t.Fatalf("resolver: %+v", b.resolver)
		}
		if err := b.Add(context.Background(), "main.go"); err != nil {
			t.Fatal(err)
		}
		result := b.Result()
		if !reflect.DeepEqual(result.ParsedFiles, []string{"main.go"}) {
			t.Fatalf("manifest consumed source budget: %+v", result.ParsedFiles)
		}
		found := false
		for _, issue := range result.Diagnostics {
			if issue.Code == "unsupported_config" && issue.Path == "go.mod" {
				found = true
			}
		}
		if !found {
			t.Fatalf("lost local replace limitation: %+v", result.Diagnostics)
		}
	}
}

func TestInvalidManifestDoesNotInventIdentity(t *testing.T) {
	files := map[string][]byte{"go.mod": []byte("module example.com/app\ninvalid directive\n"), "package.json": []byte(`{"name":`)}
	graph, err := BuildManifests(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	document, ok := graph.Document("package.json")
	if !ok || !slices.Contains(document.Tags, shared.ManifestTag) {
		t.Fatalf("lost manifest identity: %+v", document)
	}
	b, err := NewBuilder(context.Background(), BuildOptions{Files: files, Manifests: graph, MaxFiles: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.resolver.modules) != 0 || len(b.resolver.packages) != 0 {
		t.Fatal("invalid configuration resolved as a package")
	}
}

func TestSourceProjectionContainsDeclarationsNotUses(t *testing.T) {
	source := sourceFacts(t, "app.ts", []byte("import {work} from './dep'; export function run(){work();}\n"))
	if len(source.Symbols) != 1 || source.Symbols[0].Name != "run" {
		t.Fatalf("symbols: %+v", source.Symbols)
	}
}

func TestGoModuleSemanticsDoNotDependOnTags(t *testing.T) {
	g, _, err := shared.Build(context.Background(), "s", []shared.Document{{Path: "go.mod", Content: []byte("module example.com/app\n")}}, shared.Options{TagRules: []shared.TagRule{}})
	if err != nil {
		t.Fatal(err)
	}
	if got := GoModules(g); !reflect.DeepEqual(got, map[string]string{".": "example.com/app"}) {
		t.Fatal(got)
	}
}
