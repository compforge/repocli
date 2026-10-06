package project

import (
	"bytes"
	"encoding/json"
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	shared "github.com/compforge/codegraph"
	source "github.com/compforge/repocli/internal/codegraph"
	"github.com/odvcencio/gotreesitter/grammars"
)

var updateLanguages = flag.Bool("update-language-catalog", false, "regenerate the TypeScript filename catalog from pinned Go dependencies")

// The TS inspector needs filename recognition, not a second AST engine. Export
// immutable detection metadata from the pinned registry and gate drift here.
func TestTypeScriptLanguageCatalog(t *testing.T) {
	supported := map[string]bool{}
	for _, name := range shared.Languages() {
		c := shared.Capabilities(name)
		supported[name] = len(c) == 1 && len(c[0].Declarations) > 0
	}
	canonical := func(name string) string {
		if supported[name] {
			return name
		}
		return ""
	}
	registry := map[string]string{}
	for _, entry := range grammars.AllLanguages() {
		for _, ext := range entry.Extensions {
			if _, ok := registry[ext]; !ok {
				registry[ext] = canonical(grammars.DetectLanguage("source" + ext).Name)
			}
		}
	}
	// Linguist fallback tables are upstream generation output without a public
	// enumeration API. Read their Go literals only at generation/test time.
	dir, err := exec.Command("go", "list", "-f", "{{.Dir}}", "github.com/odvcencio/gotreesitter/grammars").Output()
	if err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(strings.TrimSpace(string(dir)), "linguist*.go"))
	if err != nil {
		t.Fatal(err)
	}
	tables := map[string]map[string]string{"linguistFilenames": {}, "linguistExtensions": {}}
	for _, path := range paths {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			v, ok := node.(*ast.ValueSpec)
			if !ok || len(v.Names) != 1 || len(v.Values) != 1 {
				return true
			}
			table, ok := tables[v.Names[0].Name]
			if !ok {
				return true
			}
			literal, ok := v.Values[0].(*ast.CompositeLit)
			if !ok {
				t.Fatal("upstream language table is no longer a literal")
			}
			for _, elem := range literal.Elts {
				pair := elem.(*ast.KeyValueExpr)
				key, err := strconv.Unquote(pair.Key.(*ast.BasicLit).Value)
				if err != nil {
					t.Fatal(err)
				}
				value, err := strconv.Unquote(pair.Value.(*ast.BasicLit).Value)
				if err != nil {
					t.Fatal(err)
				}
				table[key] = canonical(value)
			}
			return false
		})
	}
	if len(tables["linguistFilenames"]) == 0 || len(tables["linguistExtensions"]) == 0 {
		t.Fatal("upstream language metadata not found")
	}
	overrides := map[string]string{}
	for _, ext := range []string{".pyi", ".mjs", ".cjs", ".jsx", ".mts", ".cts"} {
		overrides[ext] = source.Language("source" + ext)
	}
	filenames := tables["linguistFilenames"]
	for _, name := range []string{"go.mod", "pyproject.toml", "package.json"} {
		filenames[name] = source.Language(name)
	}
	probes := map[string]string{}
	for _, table := range []map[string]string{registry, tables["linguistExtensions"], overrides} {
		for ext := range table {
			for _, path := range []string{"source" + ext, "nested/source" + strings.ToUpper(ext), "a.b.c.d.e" + ext, ext} {
				probes[path] = source.Language(path)
			}
		}
	}
	for name := range filenames {
		probes["nested/"+name] = source.Language("nested/" + name)
	}
	value := map[string]any{"registry": registry, "filenames": filenames, "fallback": tables["linguistExtensions"], "overrides": overrides}
	for path, value := range map[string]any{
		"../../typescript/src/languages.json":      value,
		"../../conformance/inspect/languages.json": probes,
	} {
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, '\n')
		if *updateLanguages {
			if err := os.WriteFile(path, data, 0644); err != nil {
				t.Fatal(err)
			}
		}
		existing, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, existing) {
			t.Fatal("filename catalog drift: go test ./internal/project -run TestTypeScriptLanguageCatalog -update-language-catalog")
		}
	}
}
