package codegraph

import (
	"path"
	"strconv"
	"strings"

	shared "github.com/compforge/codegraph"
)

type pythonValue struct {
	kind, text string
	items      []pythonValue
}

func (p *pythonAnalysis) eval(e shared.Expression, s *pythonState, line int) pythonValue {
	if !p.step(line) {
		return pythonValue{}
	}
	switch e.Kind {
	case "identifier":
		return s.values[e.Text]
	case "string", "integer":
		return pythonValue{kind: e.Kind, text: e.Text}
	case "true", "false":
		return pythonValue{kind: "bool", text: e.Kind}
	case "parenthesized_expression":
		if len(e.Children) == 1 {
			return p.eval(e.Children[0], s, line)
		}
	case "list", "tuple":
		v := pythonValue{kind: "list"}
		if len(e.Children) > 32 {
			// An unrelated large data literal is not a dependency gap. If it
			// feeds import/path evaluation, that operation diagnoses the unknown.
			return pythonValue{}
		}
		for _, child := range e.Children {
			v.items = append(v.items, p.eval(child, s, line))
		}
		return v
	case "attribute":
		v := p.eval(e.Children[0], s, line)
		if v.kind == "binding" {
			return pythonValue{kind: "binding", text: v.text + "." + e.Text}
		}
		if v.kind == "path" {
			if e.Text == "parent" {
				if v.text != "." {
					return pythonValue{kind: "path", text: path.Dir(v.text)}
				}
				return pythonValue{}
			}
			if e.Text == "parents" {
				return pythonValue{kind: "parents", text: v.text}
			}
			if e.Text == "resolve" || e.Text == "is_dir" {
				return pythonValue{kind: "path_method", text: e.Text, items: []pythonValue{v}}
			}
		}
	case "subscript":
		if len(e.Children) == 2 {
			v := p.eval(e.Children[0], s, line)
			index := p.eval(e.Children[1], s, line)
			n, err := strconv.Atoi(index.text)
			if v.kind == "parents" && index.kind == "integer" && err == nil && n >= 0 && n < 64 {
				for i := 0; i <= n; i++ {
					if v.text == "." {
						return pythonValue{}
					}
					v.text = path.Dir(v.text)
				}
				v.kind = "path"
				return v
			}
		}
	case "binary_operator":
		left := p.eval(e.Children[0], s, line)
		right := p.eval(e.Children[1], s, line)
		if e.Text == "/" && left.kind == "path" && right.kind == "string" && !path.IsAbs(right.text) {
			joined := path.Join(left.text, right.text)
			if joined != ".." && !strings.HasPrefix(joined, "../") {
				return pythonValue{kind: "path", text: joined}
			}
		}
		if e.Text == "+" && left.kind == "string" && right.kind == "string" {
			return pythonValue{kind: "string", text: left.text + right.text}
		}
	case "boolean_operator", "conditional_expression", "list_comprehension", "dictionary_comprehension", "set_comprehension", "generator_expression":
		branch := s.clone()
		branch.deferred = true
		for _, child := range e.Children {
			p.eval(child, branch, line)
		}
		p.merge(s, s.clone(), branch)
	case "named_expression":
		if len(e.Children) == 2 {
			value := p.eval(e.Children[1], s, line)
			p.assign(e.Children[0], value, s, line)
			return value
		}
	case "call":
		if len(e.Children) == 0 {
			return pythonValue{}
		}
		fn := p.eval(e.Children[0], s, line)
		var args []pythonValue
		for _, child := range e.Children[1:] {
			args = append(args, p.eval(child, s, line))
		}
		if fn.kind == "path_method" && len(args) == 0 {
			if fn.text == "resolve" {
				return fn.items[0]
			}
			// The captured catalog can prove a directory exists in this version.
			// Absence is unknown: ignored/external directories were not captured.
			if fn.text == "is_dir" && p.directory(fn.items[0].text) {
				return pythonValue{kind: "bool", text: "true"}
			}
		}
		if fn.kind == "path_effect" {
			p.issue("dynamic_search_path", line, "call to function with unresolved search-path effects: "+fn.text)
			p.invalidatePaths(s)
			s.mutated = true
		}
		if fn.kind != "binding" {
			return pythonValue{}
		}
		switch fn.text {
		case "str", "pathlib.Path":
			if len(args) == 1 {
				if args[0].kind == "path" || args[0].kind == "path_string" {
					value := args[0]
					value.kind = "path"
					if fn.text == "str" {
						value.kind = "path_string"
					}
					return value
				}
				if fn.text == "str" && args[0].kind == "string" {
					return args[0]
				}
			}
		case "__import__", "importlib.import_module":
			if len(args) == 1 && args[0].kind == "string" && args[0].text != "" && !strings.HasPrefix(args[0].text, ".") {
				p.reference(shared.FactImport{Path: args[0].text, Location: shared.Location{Line: line}}, s)
			} else {
				p.issue("dynamic_target", line, "runtime dependency discovery: "+fn.text)
			}
		case "sys.path.index", "sys.path.count", "sys.path.copy":
			return pythonValue{} // Read-only operations cannot invalidate known roots.
		case "sys.path.insert", "sys.path.append":
			p.pathCall(fn.text, args, s, line)
		case "eval", "exec":
			s.mutated = true
			p.issue("dynamic_target", line, "runtime dependency discovery: "+fn.text)
			p.invalidatePaths(s)
		default:
			if strings.HasPrefix(fn.text, "sys.path.") {
				s.mutated = true
				p.issue("dynamic_search_path", line, "unsupported search-path operation: "+fn.text)
				p.invalidatePaths(s)
			}
		}
	case "lambda":
		nested := s.clone()
		nested.deferred, nested.mutated = true, false
		p.invalidatePaths(nested)
		for _, child := range e.Children {
			if child.Kind == "lambda_parameters" {
				p.bindUnknown(child, nested)
			} else {
				p.eval(child, nested, line)
			}
		}
		if nested.mutated {
			return pythonValue{kind: "path_effect", text: "lambda"}
		}
		return pythonValue{} // Deferred body never mutates module context.
	default:
		for _, child := range e.Children {
			p.eval(child, s, line)
		}
	}
	return pythonValue{}
}

func (p *pythonAnalysis) pathCall(callee string, args []pythonValue, s *pythonState, line int) {
	s.mutated = true
	index := 0
	valid := len(args) == 1 && callee == "sys.path.append"
	if callee == "sys.path.insert" && len(args) == 2 && args[0].kind == "integer" {
		var err error
		index, err = strconv.Atoi(args[0].text)
		valid = err == nil && index >= 0
	}
	if !valid || args[len(args)-1].kind != "path_string" {
		p.issue("dynamic_search_path", line, "runtime dependency discovery: "+callee)
		p.invalidatePaths(s)
		return
	}
	root := args[len(args)-1].text
	if !p.directory(root) {
		p.issue("boundary_unavailable", line, "Python search path is outside captured contents: "+root)
		p.invalidatePaths(s)
		return
	}
	if callee == "sys.path.insert" && index <= len(s.paths.prefix) && !s.deferred {
		s.paths.prefix = append(s.paths.prefix, "")
		copy(s.paths.prefix[index+1:], s.paths.prefix[index:])
		s.paths.prefix[index] = root
	} else {
		if callee == "sys.path.insert" {
			p.invalidatePaths(s)
		}
		s.paths.possible = unique(append(s.paths.possible, root))
	}
}

func (p *pythonAnalysis) directory(root string) bool {
	for name := range p.resolver.files {
		if root == "." || strings.HasPrefix(name, root+"/") {
			return true
		}
	}
	return false
}
