package units

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

var outlines sync.Map // immutable programs, keyed by the exact grammar pointer

func outlineProgram(lang *gts.Language, entry *grammars.LangEntry) (*gts.Outliner, error) {
	compile := sync.OnceValues(func() (*gts.Outliner, error) {
		query := grammars.ResolveTagsQuery(*entry)
		if entry.Name == "javascript" || entry.Name == "typescript" || entry.Name == "tsx" {
			query += `
(variable_declarator name: (identifier) @name) @definition.variable
`
			if entry.Name == "javascript" {
				query += `(field_definition property: [(property_identifier) (private_property_identifier)] @name) @definition.field`
			} else {
				query += `
(public_field_definition name: [(property_identifier) (private_property_identifier)] @name) @definition.field
(property_signature name: [(property_identifier) (private_property_identifier)] @name) @definition.property
(method_signature name: (property_identifier) @name) @definition.method
`
			}
		}
		return gts.NewOutliner(lang, query, gts.WithOutlineOwnerRules(grammars.OutlineOwnerRules(*entry)))
	})
	v, _ := outlines.LoadOrStore(lang, compile)
	return v.(func() (*gts.Outliner, error))()
}

// sourceElements uses syntax only: no name binding, repository I/O or graph.
func sourceElements(ctx context.Context, path, source string) ([]Element, []string) {
	if err := ctx.Err(); err != nil {
		return nil, []string{err.Error()}
	}
	if len(source) > 2<<20 {
		return nil, []string{"source exceeds 2 MiB syntax limit"}
	}
	if strings.HasSuffix(path, ".go") {
		return goElements(path, source)
	}
	entry := grammars.DetectLanguage(path)
	if entry == nil {
		return nil, []string{"unsupported source syntax"}
	}
	lang := entry.Language()
	parser := gts.NewParser(lang)
	parser.SetTimeoutMicros(2_000_000)
	var canceled uint32
	parser.SetCancellationFlag(&canceled)
	stop := context.AfterFunc(ctx, func() { atomic.StoreUint32(&canceled, 1) })
	defer stop()
	var tree *gts.Tree
	var err error
	if entry.TokenSourceFactory != nil {
		tree, err = parser.ParseWithTokenSourceStrict([]byte(source), entry.TokenSourceFactory([]byte(source), lang))
	} else {
		tree, err = parser.ParseStrict([]byte(source))
	}
	if err != nil {
		return nil, []string{fmt.Sprintf("parse source: %v", err)}
	}
	defer tree.Release()
	program, err := outlineProgram(lang, entry)
	if err != nil {
		return nil, []string{fmt.Sprintf("compile source outline: %v", err)}
	}
	symbols, report := program.OutlineTree(tree)
	var gaps []string
	if report.Declined() || report.Truncated || report.Omitted() > report.OmittedDuplicate {
		gaps = append(gaps, "source outline incomplete")
	}
	if tree.RootNode().HasError() {
		gaps = append(gaps, "source contains syntax errors")
	}
	var out []Element
	var flatten func([]gts.OutlineSymbol, string, Kind)
	flatten = func(items []gts.OutlineSymbol, parent string, parentKind Kind) {
		for _, item := range items {
			kind := syntaxKind(item.Kind)
			if item.NodeType == "struct_item" || item.NodeType == "struct_specifier" {
				kind = Struct
			}
			if kind == Function && parentKind == Class {
				kind = Method
			}
			name := item.Name
			if parent != "" {
				name = parent + "." + name
			} else if item.Owner != "" {
				name = item.Owner + "." + name
			}
			out = append(out, Element{Path: path, Name: name, Kind: kind, Span: pointSpan(item.Range.StartPoint, item.Range.EndPoint), StartByte: int(item.Range.StartByte), EndByte: int(item.Range.EndByte)})
			flatten(item.Children, name, kind)
		}
	}
	flatten(symbols, "", Unknown)
	// Import syntax is not a declaration outline. Recognize grammar nodes rather
	// than guessing dependencies from the spelling of identifiers or source text.
	var walk func(*gts.Node)
	walk = func(n *gts.Node) {
		kind := Unknown
		switch n.Type(lang) {
		case "import_statement", "import_from_statement", "import_declaration", "use_declaration", "using_directive", "preproc_include":
			kind = Import
		}
		if kind != Unknown {
			out = append(out, Element{Path: path, Kind: kind, Span: pointSpan(n.StartPoint(), n.EndPoint()), StartByte: int(n.StartByte()), EndByte: int(n.EndByte())})
			return
		}
		for i := 0; i < int(n.NamedChildCount()); i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(tree.RootNode())
	sortElementsBySize(out)
	return out, gaps
}

func syntaxKind(kind string) Kind {
	k := Kind(kind)
	switch k {
	case Function, Method, Class, Struct, Interface, Import, Export, Type, Enum, Field, Property, Variable, Constant, Module, Namespace, Constructor, Record:
		return k
	default:
		return Unknown
	}
}
func pointSpan(start, end gts.Point) Span {
	last := int(end.Row) + 1
	if end.Column == 0 && end.Row > start.Row {
		last--
	}
	return Span{int(start.Row) + 1, last}
}
