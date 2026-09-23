package codegraph

import (
	"context"
	"maps"
	"reflect"
	"slices"
	"strings"

	shared "github.com/compforge/codegraph"
)

type pythonState struct {
	values   map[string]pythonValue
	paths    pythonPaths
	deferred bool
	mutated  bool
}

func (s *pythonState) clone() *pythonState {
	return &pythonState{values: maps.Clone(s.values), paths: pythonPaths{prefix: slices.Clone(s.paths.prefix), possible: slices.Clone(s.paths.possible)}, deferred: s.deferred, mutated: s.mutated}
}

type pythonAnalysis struct {
	ctx       context.Context
	resolver  *resolver
	name      string
	remaining int
	exhausted bool
	imports   []resolvedImport
	issues    []Diagnostic
}

func (r *resolver) pythonImports(ctx context.Context, name string, program []shared.Statement) ([]resolvedImport, []Diagnostic) {
	p := pythonAnalysis{ctx: ctx, resolver: r, name: name, remaining: 20000}
	s := &pythonState{values: map[string]pythonValue{"__file__": {kind: "path_string", text: name}}}
	for _, name := range []string{"str", "__import__", "eval", "exec"} {
		s.values[name] = pythonValue{kind: "binding", text: name}
	}
	p.statements(program, s)
	return p.imports, p.issues
}

func (p *pythonAnalysis) step(line int) bool {
	p.remaining--
	if p.ctx.Err() != nil || p.remaining < 0 {
		if !p.exhausted {
			p.issue("context_limit", line, "Python context evaluation limit reached")
			p.exhausted = true
		}
		return false
	}
	return true
}

func (p *pythonAnalysis) issue(code string, line int, message string) {
	p.issues = append(p.issues, Diagnostic{Path: p.name, Kind: Imports, Code: code, Line: line, Message: message})
}

func (p *pythonAnalysis) invalidatePaths(s *pythonState) {
	s.paths.possible = unique(append(s.paths.possible, s.paths.prefix...))
	s.paths.prefix = nil
}

func (p *pythonAnalysis) reference(imp shared.FactImport, s *pythonState) resolvedImport {
	result := p.resolver.pythonResolve(p.name, imp, s.paths)
	if len(result.targets) > 0 {
		p.imports = append(p.imports, result)
	}
	return result
}

func (p *pythonAnalysis) statements(statements []shared.Statement, s *pythonState) {
	for _, stmt := range statements {
		if !p.step(stmt.Line) {
			return
		}
		switch stmt.Kind {
		case "import_statement", "import_from_statement", "future_import_statement":
			for _, imp := range stmt.Imports {
				resolved := p.reference(imp, s)
				binding := imp.Alias
				if binding == "" {
					binding = imp.Binding
					if len(imp.Names) == 0 {
						binding = strings.Split(imp.Path, ".")[0]
					}
				}
				value := pythonValue{}
				if imp.Relative == 0 && len(resolved.targets) == 0 {
					switch imp.Path {
					case "sys", "pathlib", "pathlib.Path", "importlib", "importlib.import_module":
						value = pythonValue{kind: "binding", text: imp.Path}
					}
				}
				if binding == "*" {
					for name := range s.values {
						if name != "__file__" {
							s.values[name] = pythonValue{}
						}
					}

				} else {
					s.values[binding] = value
				}
			}
		case "assignment":
			value := p.eval(stmt.Value, s, stmt.Line)
			p.assign(stmt.Target, value, s, stmt.Line)
		case "augmented_assignment", "delete_statement":
			p.eval(stmt.Value, s, stmt.Line)
			p.assign(stmt.Target, pythonValue{}, s, stmt.Line)
		case "if":
			condition := p.eval(stmt.Value, s, stmt.Line)
			if condition.kind == "bool" {
				if condition.text == "true" {
					p.statements(stmt.Body, s)
				} else {
					p.statements(stmt.Else, s)
				}
			} else {
				// Analyze both branches as possible contexts. Paths/values are
				// retained as certain after the join only when both agree.
				left, right := s.clone(), s.clone()
				start := len(p.imports)
				p.statements(stmt.Body, left)
				p.statements(stmt.Else, right)
				for i := start; i < len(p.imports); i++ {
					if p.imports[i].confidence == "" {
						p.imports[i].confidence = Strong
						p.imports[i].basis = "python_conditional_context"
					}
				}
				p.merge(s, left, right)
			}
		case "for_statement":
			sequence := p.eval(stmt.Value, s, stmt.Line)
			if sequence.kind == "list" && !pythonAbrupt(stmt.Body) {
				for _, value := range sequence.items {
					p.assign(stmt.Target, value, s, stmt.Line)
					p.statements(stmt.Body, s)
				}
				p.statements(stmt.Else, s)
			} else {
				branch := s.clone()
				branch.mutated = false
				p.assign(stmt.Target, pythonValue{}, branch, stmt.Line)
				p.invalidatePaths(branch)
				branch.deferred = true
				p.statements(stmt.Body, branch)
				p.statements(stmt.Else, branch)
				if !branch.mutated {
					branch.paths = s.paths
				}
				p.merge(s, s.clone(), branch)
			}
		case "function_definition", "class_definition":
			for _, expression := range stmt.Prelude {
				p.eval(expression, s, stmt.Line)
			}
			s.values[stmt.Name] = pythonValue{}
			nested := s.clone()
			nested.mutated = false
			nested.deferred = true
			p.invalidatePaths(nested)
			p.bindUnknown(stmt.Target, nested)
			// Python determines local bindings for the whole function, including
			// assignments textually after an import or call.
			if stmt.Kind == "function_definition" {
				p.localBindings(stmt.Body, nested)
			}
			p.statements(stmt.Body, nested)
			if stmt.Kind == "class_definition" && nested.mutated {
				p.invalidatePaths(s)
				s.mutated = true
			}
			if stmt.Kind == "function_definition" && nested.mutated {
				s.values[stmt.Name] = pythonValue{kind: "path_effect", text: stmt.Name}
			}
		case "opaque":
			branch := s.clone()
			branch.mutated = false
			branch.deferred = true
			p.invalidatePaths(branch)
			p.statements(stmt.Body, branch)
			// Control flow alone is not an unresolved dependency. Its imports
			// and path effects already carry their own uncertainty.
			if !branch.mutated {
				branch.paths = s.paths
			}
			p.merge(s, s.clone(), branch)
		default:
			p.eval(stmt.Value, s, stmt.Line)
		}
	}
}

func (p *pythonAnalysis) assign(target shared.Expression, value pythonValue, s *pythonState, line int) {
	if target.Kind == "delete_statement" {
		for _, child := range target.Children {
			p.assign(child, pythonValue{}, s, line)
		}
		return
	}
	if target.Kind == "identifier" {
		s.values[target.Text] = value
		return
	}
	if (target.Kind == "tuple_pattern" || target.Kind == "pattern_list" || target.Kind == "tuple" || target.Kind == "list_pattern") && value.kind == "list" && len(target.Children) == len(value.items) {
		for i, child := range target.Children {
			p.assign(child, value.items[i], s, line)
		}
		return
	}
	if target.Kind == "attribute" || target.Kind == "subscript" {
		resolved := p.eval(target, s, line)
		if target.Kind == "subscript" && len(target.Children) > 0 {
			resolved = p.eval(target.Children[0], s, line)
		}
		if resolved.kind == "binding" && strings.HasPrefix(resolved.text, "sys.path") {
			p.invalidatePaths(s)
			s.mutated = true
		}
		return
	}
	p.bindUnknown(target, s)
}

func (p *pythonAnalysis) bindUnknown(target shared.Expression, s *pythonState) {
	if target.Kind == "identifier" {
		s.values[target.Text] = pythonValue{}
	}
	for _, child := range target.Children {
		p.bindUnknown(child, s)
	}
}

func (p *pythonAnalysis) localBindings(statements []shared.Statement, s *pythonState) {
	for _, stmt := range statements {
		switch stmt.Kind {
		case "assignment", "augmented_assignment", "for_statement", "delete_statement":
			p.bindUnknown(stmt.Target, s)
		case "function_definition", "class_definition":
			s.values[stmt.Name] = pythonValue{}
			continue
		case "import_statement", "import_from_statement":
			for _, imp := range stmt.Imports {
				name := imp.Alias
				if name == "" {
					name = imp.Binding
					if len(imp.Names) == 0 {
						name = strings.Split(imp.Path, ".")[0]
					}
				}
				s.values[name] = pythonValue{}
			}
		}
		p.localBindings(stmt.Body, s)
		p.localBindings(stmt.Else, s)
	}
}

func (p *pythonAnalysis) merge(out, left, right *pythonState) {
	out.mutated = left.mutated || right.mutated
	for name, value := range left.values {
		if reflect.DeepEqual(value, right.values[name]) {
			out.values[name] = value
		} else {
			out.values[name] = pythonValue{}
		}
	}
	for name := range right.values {
		if _, ok := left.values[name]; !ok {
			out.values[name] = pythonValue{}
		}
	}
	if reflect.DeepEqual(left.paths, right.paths) {
		out.paths = left.paths
	} else {
		// Shared ordered prefixes remain definite; divergent suffixes are only
		// possible roots. This never mixes branch halves into a proven route.
		common := 0
		for common < len(left.paths.prefix) && common < len(right.paths.prefix) && left.paths.prefix[common] == right.paths.prefix[common] {
			common++
		}
		out.paths.prefix = slices.Clone(left.paths.prefix[:common])
		out.paths.possible = unique(append(append(append(slices.Clone(left.paths.possible), right.paths.possible...), left.paths.prefix[common:]...), right.paths.prefix[common:]...))
	}
}

func pythonAbrupt(statements []shared.Statement) bool {
	for _, stmt := range statements {
		switch stmt.Kind {
		case "break_statement", "continue_statement", "return_statement", "raise_statement":
			return true
		case "function_definition", "class_definition":
			continue
		}
		if pythonAbrupt(stmt.Body) || pythonAbrupt(stmt.Else) {
			return true
		}
	}
	return false
}
