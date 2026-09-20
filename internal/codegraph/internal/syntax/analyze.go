// Package syntax turns syntax trees into facts owned by repocli.
package syntax

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	gs "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// Symbol identifies a declaration inside one source file.
// +spec=`Qualified names retain ownership; line numbers locate one version only`
type Symbol struct {
	QualifiedName string `json:"qualifiedName"`
	Parent        string `json:"-"`
	StartByte     uint32 `json:"-"`
	EndByte       uint32 `json:"-"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	StartLine     int    `json:"startLine"`
	EndLine       int    `json:"endLine"`
}

type Import struct {
	Alias     string
	Binding   string // Name introduced in this lexical scope.
	StartByte uint32
	Line      int // One-based source location; zero when an adapter has no location.
	Path      string
	From      string
	Relative  int
	Names     []string // Imported names, before caller aliases; empty means whole module.
}

type LocalCall struct {
	Caller, Callee string
	Line           int
}

type Facts struct {
	Calls    []LocalCall
	Language string
	Symbols  []Symbol
	Imports  []Import
	Issues   []Issue
	Python   []PythonStatement // Ordered, tree-independent Python context facts.
	Exports  map[string]string // Public name -> local name (for export aliases).
}

// Analyzer reuses facts for identical before/after content within one analysis.
// Trees are released immediately; callers receive only immutable value facts.
type Analyzer struct {
	cache     map[[32]byte]Facts
	programs  map[programKey]*gs.FactProgram
	outliners map[*gs.Language]*gs.Outliner
}

func Language(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".go":
		return "go"
	case ".py", ".pyi":
		return "python"
	case ".ts", ".mts", ".cts":
		return "typescript"
	case ".tsx":
		return "tsx"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	}
	return ""
}

func (a *Analyzer) Analyze(ctx context.Context, name string, source []byte) Facts {
	return a.AnalyzeFeatures(ctx, name, source, Features{Symbols: true, Calls: true})
}

// AnalyzeFeatures always extracts imports. Outlines and local calls are optional;
// the feature set participates in caching so a shallow result cannot hide facts.
func (a *Analyzer) AnalyzeFeatures(ctx context.Context, name string, source []byte, features Features) Facts {
	language := Language(name)
	key := sha256.Sum256(append([]byte(fmt.Sprintf("%s\x00%s\x00%t:%t\x00", language, name, features.Symbols, features.Calls)), source...))
	if f, ok := a.cache[key]; ok {
		return f
	}
	f := Facts{Language: language, Symbols: []Symbol{}, Imports: []Import{}, Exports: map[string]string{}}
	if language == "" {
		return f
	}
	if err := ctx.Err(); err != nil {
		f.issue("cancelled", "", 0, err.Error())
		return f
	}
	entry := grammars.DetectLanguageByName(language)
	lang := entry.Language()
	parser := gs.NewParser(lang)
	timeout := 2 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		timeout = min(timeout, time.Until(deadline))
	}
	parser.SetTimeoutMicros(uint64(max(timeout/time.Microsecond, 1)))
	var tree *gs.Tree
	var err error
	if entry.TokenSourceFactory != nil {
		tree, err = parser.ParseWithTokenSourceStrict(source, entry.TokenSourceFactory(source, lang))
	} else {
		tree, err = parser.ParseStrict(source)
	}
	if tree != nil {
		defer tree.Release()
	}
	if err != nil {
		f.issue("parse_error", "", 0, fmt.Sprintf("syntax parse failed: %v", err))
		return f
	}
	if tree == nil || tree.RootNode() == nil {
		f.issue("parse_error", "", 0, "empty syntax tree")
		return f
	}
	if tree.RootNode().HasErrorOrMissing() {
		f.issue("parse_error", "", 0, "syntax tree contains errors or missing nodes")
	}
	if features.Symbols || features.Calls {
		a.extractSymbols(&f, tree, *entry)
	}
	extracted, err := a.extractFacts(tree, features.Calls && language != "go")
	if err != nil {
		f.issue("extraction_error", "", 0, "syntax facts: "+err.Error())
		return f
	}
	for _, i := range extracted.Imports {
		if i.Kind != "package" {
			imp := Import{StartByte: i.StartByte, Alias: i.Alias, Binding: i.Name, Path: i.Path, From: i.From, Relative: i.Relative, Line: bytes.Count(source[:i.StartByte], []byte("\n")) + 1}
			if i.Kind == "from_import" && !i.Wildcard {
				imp.Names = []string{i.Name}
			}
			f.Imports = append(f.Imports, imp)
		}
	}
	walk(tree.RootNode(), func(n *gs.Node) {
		typ := n.Type(lang)
		if language == "typescript" || language == "tsx" || language == "javascript" {
			switch typ {
			case "import_statement", "export_statement":
				if src := n.ChildByFieldName("source", lang); src != nil {
					start := len(f.Imports)
					addJSImport(&f, src, lang, source)
					if len(f.Imports) > start {
						f.Imports[start].Names = importedNames(n, lang, source)
					}
				} else if typ == "export_statement" {
					walk(n, func(child *gs.Node) {
						if child.Type(lang) == "export_specifier" {
							local := child.ChildByFieldName("name", lang)
							alias := child.ChildByFieldName("alias", lang)
							if local != nil && alias != nil {
								f.Exports[alias.Text(source)] = local.Text(source)
							}
						}
					})
				}
			case "call_expression":
				fn := n.ChildByFieldName("function", lang)
				if fn == nil {
					break
				}
				callee := fn.Text(source)
				if callee == "import" || callee == "require" || callee == "require.resolve" {
					args := n.ChildByFieldName("arguments", lang)
					if args == nil || args.NamedChildCount() == 0 {
						f.issue("dynamic_target", Imports, int(n.StartPoint().Row)+1, "dynamic import has no static target")
					} else {
						addJSImport(&f, args.NamedChild(0), lang, source)
					}
				} else if callee == "eval" || callee == "import.meta.glob" || callee == "require.context" {
					f.issue("dynamic_target", Imports, int(n.StartPoint().Row)+1, "runtime dependency discovery: "+callee)
				}
			}
		}
		if language == "python" {
			if typ == "decorated_definition" {
				for i := range f.Symbols {
					if f.Symbols[i].StartLine > int(n.StartPoint().Row)+1 && f.Symbols[i].EndLine <= int(n.EndPoint().Row)+1 {
						f.Symbols[i].StartLine = int(n.StartPoint().Row) + 1
					}
				}
			}

		}
	})
	if language == "python" {
		f.Python = pythonProgram(&f, tree)
	}
	if features.Calls {
		extractLocalCalls(&f, tree, extracted.Calls)
	}
	if language == "go" && bytes.Contains(source, []byte("//go:embed")) {
		f.issue("unsupported_resource", Imports, 0, "go:embed dependencies are not resolved")
	}
	sort.Slice(f.Symbols, func(i, j int) bool {
		if f.Symbols[i].StartLine != f.Symbols[j].StartLine {
			return f.Symbols[i].StartLine < f.Symbols[j].StartLine
		}
		return f.Symbols[i].Name < f.Symbols[j].Name
	})
	if a.cache == nil {
		a.cache = map[[32]byte]Facts{}
	}
	a.cache[key] = f
	return f
}

func addJSImport(f *Facts, n *gs.Node, lang *gs.Language, source []byte) {
	raw := n.Text(source)
	if n.Type(lang) != "string" || len(raw) < 2 || strings.Contains(raw, "\\") {
		f.issue("dynamic_target", Imports, int(n.StartPoint().Row)+1, "import target is not a plain string literal")
		return
	}
	f.Imports = append(f.Imports, Import{Path: raw[1 : len(raw)-1], Line: int(n.StartPoint().Row) + 1})
}

func importedNames(n *gs.Node, lang *gs.Language, source []byte) []string {
	var names []string
	whole := false
	walk(n, func(child *gs.Node) {
		switch child.Type(lang) {
		case "import_specifier", "export_specifier":
			if name := child.ChildByFieldName("name", lang); name != nil {
				names = append(names, name.Text(source))
			}
		case "namespace_import":
			whole = true
		case "import_clause":
			for i := 0; i < child.NamedChildCount(); i++ {
				if child.NamedChild(i).Type(lang) == "identifier" {
					whole = true
				}
			}
		}
	})
	if whole {
		return nil
	}
	return names
}

func walk(root *gs.Node, visit func(*gs.Node)) {
	stack := []*gs.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		visit(n)
		for i := n.NamedChildCount() - 1; i >= 0; i-- {
			stack = append(stack, n.NamedChild(i))
		}
	}
}
