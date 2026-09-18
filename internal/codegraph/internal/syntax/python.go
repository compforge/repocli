package syntax

import (
	"strings"

	gs "github.com/odvcencio/gotreesitter"
)

// PythonStatement preserves execution order and scope without evaluating code.
// Its bounded expression vocabulary is interpreted inside CodeGraph.
type PythonStatement struct {
	Kind, Name    string
	Line          int
	Imports       []Import
	Target, Value PythonExpression
	Prelude       []PythonExpression
	Body, Else    []PythonStatement
}

type PythonExpression struct {
	Kind, Text string
	Children   []PythonExpression
}

func pythonProgram(f *Facts, tree *gs.Tree) []PythonStatement {
	lang, source := tree.Language(), tree.Source()
	imports := map[uint32][]Import{}
	for _, imp := range f.Imports {
		imports[imp.StartByte] = append(imports[imp.StartByte], imp)
	}
	remaining := 10000
	exhausted := false
	take := func() bool {
		remaining--
		if remaining < 0 {
			exhausted = true
			return false
		}
		return true
	}
	var expr func(*gs.Node, int) PythonExpression
	expr = func(n *gs.Node, depth int) PythonExpression {
		if n == nil {
			return PythonExpression{}
		}
		if depth > 64 || !take() {
			exhausted = true
			return PythonExpression{Kind: "unknown"}
		}
		e := PythonExpression{Kind: n.Type(lang)}
		switch e.Kind {
		case "identifier", "integer", "true", "false", "none":
			e.Text = n.Text(source)
		case "string":
			raw := n.Text(source)
			if len(raw) >= 2 && (raw[0] == '\'' || raw[0] == '"') && raw[len(raw)-1] == raw[0] && !strings.ContainsAny(raw[1:len(raw)-1], "\\\n\r") && !strings.Contains(raw[1:len(raw)-1], string(raw[0])) {
				e.Text = raw[1 : len(raw)-1]
			} else {
				e.Kind = "unknown"
				for i := 0; i < n.NamedChildCount(); i++ {
					e.Children = append(e.Children, expr(n.NamedChild(i), depth+1))
				}
			}
		case "attribute":
			if attr := n.ChildByFieldName("attribute", lang); attr != nil {
				e.Text = attr.Text(source)
			}
			e.Children = []PythonExpression{expr(n.ChildByFieldName("object", lang), depth+1)}
		case "binary_operator":
			if op := n.ChildByFieldName("operator", lang); op != nil {
				e.Text = op.Text(source)
			}
			e.Children = []PythonExpression{expr(n.ChildByFieldName("left", lang), depth+1), expr(n.ChildByFieldName("right", lang), depth+1)}
		case "call":
			e.Children = []PythonExpression{expr(n.ChildByFieldName("function", lang), depth+1)}
			if args := n.ChildByFieldName("arguments", lang); args != nil {
				for i := 0; i < args.NamedChildCount(); i++ {
					e.Children = append(e.Children, expr(args.NamedChild(i), depth+1))
				}
			}
		default:
			for i := 0; i < n.NamedChildCount(); i++ {
				e.Children = append(e.Children, expr(n.NamedChild(i), depth+1))
			}
		}
		return e
	}
	var statements func(*gs.Node, int) []PythonStatement
	statements = func(n *gs.Node, depth int) []PythonStatement {
		if n == nil {
			return nil
		}
		if depth > 64 || !take() {
			exhausted = true
			return nil
		}
		kind := n.Type(lang)
		s := PythonStatement{Kind: kind, Line: int(n.StartPoint().Row) + 1}
		switch kind {
		case "module", "block", "expression_statement", "else_clause", "finally_clause":
			var out []PythonStatement
			for i := 0; i < n.NamedChildCount(); i++ {
				out = append(out, statements(n.NamedChild(i), depth+1)...)
			}
			return out
		case "import_statement", "import_from_statement", "future_import_statement":
			s.Imports = imports[n.StartByte()]
		case "assignment", "augmented_assignment":
			s.Target = expr(n.ChildByFieldName("left", lang), 0)
			s.Value = expr(n.ChildByFieldName("right", lang), 0)
		case "if_statement", "elif_clause":
			s.Kind = "if"
			s.Value = expr(n.ChildByFieldName("condition", lang), 0)
			s.Body = statements(n.ChildByFieldName("consequence", lang), depth+1)
			s.Else = statements(n.ChildByFieldName("alternative", lang), depth+1)
		case "for_statement":
			s.Target = expr(n.ChildByFieldName("left", lang), 0)
			s.Value = expr(n.ChildByFieldName("right", lang), 0)
			s.Body = statements(n.ChildByFieldName("body", lang), depth+1)
			s.Else = statements(n.ChildByFieldName("alternative", lang), depth+1)
		case "function_definition", "class_definition":
			if name := n.ChildByFieldName("name", lang); name != nil {
				s.Name = name.Text(source)
			}
			params := n.ChildByFieldName("parameters", lang)
			s.Target = expr(params, 0)
			if params != nil {
				for i := 0; i < params.NamedChildCount(); i++ {
					param := params.NamedChild(i)
					if value := param.ChildByFieldName("value", lang); value != nil {
						s.Prelude = append(s.Prelude, expr(value, 0))
					}
				}
			}
			if bases := n.ChildByFieldName("superclasses", lang); bases != nil {
				s.Prelude = append(s.Prelude, expr(bases, 0))
			}
			s.Body = statements(n.ChildByFieldName("body", lang), depth+1)
		case "decorated_definition":
			var out []PythonStatement
			for i := 0; i < n.NamedChildCount(); i++ {
				child := n.NamedChild(i)
				if child.Type(lang) == "decorator" {
					out = append(out, PythonStatement{Kind: "expression", Line: int(child.StartPoint().Row) + 1, Value: expr(child, 0)})
				}
			}
			return append(out, statements(n.ChildByFieldName("definition", lang), depth+1)...)

		case "try_statement", "while_statement", "with_statement", "match_statement", "except_clause", "case_clause":
			s.Kind = "opaque"
			for i := 0; i < n.NamedChildCount(); i++ {
				s.Body = append(s.Body, statements(n.NamedChild(i), depth+1)...)
			}
		case "delete_statement":
			s.Target = expr(n, 0)
		default:
			s.Value = expr(n, 0)
		}
		return []PythonStatement{s}
	}
	result := statements(tree.RootNode(), 0)
	if exhausted {
		f.issue("context_limit", Imports, 0, "Python context extraction limit reached")
	}
	return result
}
