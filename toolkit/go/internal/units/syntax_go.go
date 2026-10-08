package units

import (
	"go/ast"
	"go/parser"
	"go/token"
)

// Go's native parser distinguishes named structs, interfaces and aliases,
// including declarations inside grouped type/import blocks.
func goElements(path, source string) ([]Element, []string) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, source, parser.ParseComments|parser.SkipObjectResolution)
	var gaps []string
	if err != nil {
		gaps = append(gaps, "source contains syntax errors")
	}
	if file == nil {
		return nil, gaps
	}
	var out []Element
	add := func(node ast.Node, doc *ast.CommentGroup, name string, kind Kind) {
		start := node.Pos()
		if doc != nil {
			start = doc.Pos()
		}
		first, last := fset.Position(start), fset.Position(node.End())
		out = append(out, Element{Path: path, Name: name, Kind: kind, Span: Span{first.Line, last.Line}, StartByte: first.Offset, EndByte: last.Offset})
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			kind := Function
			name := d.Name.Name
			if d.Recv != nil {
				kind = Method
				name = receiverName(d.Recv.List[0].Type) + "." + name
			}
			add(d, d.Doc, name, kind)
		case *ast.GenDecl:
			// The enclosing declaration owns parentheses and keywords; each spec can
			// still form its own fragment when edits are wholly within that spec.
			outerKind := Unknown
			switch d.Tok {
			case token.IMPORT:
				outerKind = Import
			case token.TYPE:
				outerKind = Type
			case token.CONST:
				outerKind = Constant
			case token.VAR:
				outerKind = Variable
			}
			if len(d.Specs) > 1 {
				add(d, d.Doc, "", outerKind)
			}
			for _, spec := range d.Specs {
				node := ast.Node(spec)
				doc := d.Doc
				if len(d.Specs) == 1 {
					node = d
				}
				switch s := spec.(type) {
				case *ast.ImportSpec:
					if s.Doc != nil {
						doc = s.Doc
					}
					add(node, doc, "", Import)
				case *ast.TypeSpec:
					if s.Doc != nil {
						doc = s.Doc
					}
					kind := Type
					switch s.Type.(type) {
					case *ast.StructType:
						kind = Struct
					case *ast.InterfaceType:
						kind = Interface
					}
					add(node, doc, s.Name.Name, kind)
					var fields *ast.FieldList
					memberKind := Field
					switch typ := s.Type.(type) {
					case *ast.StructType:
						fields = typ.Fields
					case *ast.InterfaceType:
						fields = typ.Methods
						memberKind = Method
					}
					if fields != nil {
						for _, field := range fields.List {
							if len(field.Names) == 1 {
								add(field, field.Doc, s.Name.Name+"."+field.Names[0].Name, memberKind)
							}
						}
					}

				case *ast.ValueSpec:
					if s.Doc != nil {
						doc = s.Doc
					}
					name := ""
					if len(s.Names) > 0 {
						name = s.Names[0].Name
					}
					add(node, doc, name, outerKind)
				}
			}
		}
	}
	sortElementsBySize(out)
	return out, gaps
}
func receiverName(e ast.Expr) string {
	switch n := e.(type) {
	case *ast.Ident:
		return n.Name
	case *ast.StarExpr:
		return receiverName(n.X)
	case *ast.IndexExpr:
		return receiverName(n.X)
	case *ast.IndexListExpr:
		return receiverName(n.X)
	}
	return ""
}
