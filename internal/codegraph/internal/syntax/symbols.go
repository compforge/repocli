package syntax

import (
	"fmt"
	"strings"

	gs "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// extractSymbols consumes upstream ownership facts. Grammar shapes and receiver
// rules stay with gotreesitter; repocli adds repository-relative file identity.
func extractSymbols(f *Facts, tree *gs.Tree, entry grammars.LangEntry) {
	query := grammars.ResolveTagsQuery(entry)
	// Upstream tags omit variable declarators and TS aliases. Extend the query,
	// keeping ranges and lexical ownership in the upstream outliner.
	switch f.Language {
	case "javascript", "typescript", "tsx":
		query += "\n(variable_declarator name: (identifier) @name) @definition.variable"
		if f.Language != "javascript" {
			query += "\n(type_alias_declaration name: (type_identifier) @name) @definition.type"
		}
	}
	outliner, err := gs.NewOutliner(tree.Language(), query,
		gs.WithOutlineOwnerRules(grammars.OutlineOwnerRules(entry)))
	if err != nil {
		f.issue("outline_incomplete", Symbols, 0, "symbol outline: "+err.Error())
		return
	}
	symbols, report := outliner.OutlineTree(tree)
	if report.Declined() || report.Truncated || report.Omitted() > 0 || report.OwnerRuleMisses > 0 {
		f.issue("outline_incomplete", Symbols, 0, fmt.Sprintf("symbol outline is incomplete: %+v", report))
	}
	var flatten func([]gs.OutlineSymbol, string)
	flatten = func(symbols []gs.OutlineSymbol, parent string) {
		for _, item := range symbols {
			owner := parent
			if item.Owner != "" {
				owner = item.Owner
			}
			qualified := item.Name
			if owner != "" {
				qualified = owner + "." + item.Name
			}
			// Range.EndPoint is exclusive, including when it starts a new line.
			end := int(item.Range.EndPoint.Row) + 1
			if item.Range.EndPoint.Column == 0 && end > int(item.Range.StartPoint.Row)+1 {
				end--
			}
			f.Symbols = append(f.Symbols, Symbol{Name: item.Name, QualifiedName: qualified,
				Parent: owner, Kind: item.Kind, StartByte: item.Range.StartByte, EndByte: item.Range.EndByte,
				StartLine: int(item.Range.StartPoint.Row) + 1, EndLine: end})
			flatten(item.Children, qualified)
		}
	}
	flatten(symbols, "")
}

// extractLocalCalls resolves a deliberately bounded subset: direct identifier
// calls to unique module-level functions. The upstream extractor owns call-site
// syntax. Binding uncertainty is a diagnostic, never a guessed call edge.
// +why=`A same-name method or shadowed binding is not evidence of a call`
func extractLocalCalls(f *Facts, tree *gs.Tree) {
	if f.Language == "go" || tree.RootNode().HasErrorOrMissing() {
		return
	} // Go impact uses packages.
	lang := tree.Language()
	definitions := map[string][]Symbol{}
	for _, symbol := range f.Symbols {
		if symbol.Parent == "" && symbol.Kind == "function" {
			definitions[symbol.Name] = append(definitions[symbol.Name], symbol)
		}
	}
	for _, call := range gs.ExtractCalls(tree) {
		if call.Receiver != "" {
			continue
		}
		candidates := definitions[call.Name]
		if len(candidates) == 0 {
			continue
		}
		var caller *Symbol
		for i := range f.Symbols {
			symbol := &f.Symbols[i]
			if symbol.StartByte <= call.StartByte && symbol.EndByte >= call.EndByte &&
				(caller == nil || symbol.EndByte-symbol.StartByte < caller.EndByte-caller.StartByte) {
				caller = symbol
			}
		}
		if caller == nil {
			continue
		}
		n := tree.NamedNodeAtByte(call.StartByte)
		for n != nil && (n.StartByte() != call.StartByte || n.EndByte() != call.EndByte) {
			n = n.Parent()
		}
		if n == nil {
			continue
		}
		fn := n.ChildByFieldName("function", lang)
		if fn == nil || fn.Type(lang) != "identifier" {
			continue
		}
		line := int(n.StartPoint().Row) + 1
		if len(candidates) != 1 || hasBindingConflict(tree, n, candidates[0], call.Name) {
			f.Issues = append(f.Issues, Issue{Code: "binding_ambiguous", Feature: Calls, Line: line, Symbol: caller.QualifiedName, Message: fmt.Sprintf("local call %s at line %d has unresolved bindings", call.Name, line)})
			continue
		}
		f.Calls = append(f.Calls, LocalCall{Caller: caller.QualifiedName, Callee: candidates[0].QualifiedName, Line: line})
	}
}

// hasBindingConflict checks lexical ancestors only. Unrelated function bodies
// cannot shadow a module binding. Unsupported binding forms fail closed.
func hasBindingConflict(tree *gs.Tree, call *gs.Node, target Symbol, name string) bool {
	lang, source := tree.Language(), tree.Source()
	conflict := false
	var visit func(*gs.Node)
	visit = func(n *gs.Node) {
		if conflict {
			return
		}
		typ := n.Type(lang)
		contains := n.StartByte() <= call.StartByte() && n.EndByte() >= call.EndByte()
		scope := typ == "function_definition" || typ == "function_declaration" || typ == "method_definition" ||
			typ == "class_definition" || typ == "class_declaration" || typ == "arrow_function" ||
			typ == "function_expression" || typ == "lambda" || strings.HasPrefix(typ, "generator_function")
		if scope && !contains {
			id := n.ChildByFieldName("name", lang)
			if id != nil && id.Text(source) == name &&
				(n.StartByte() != target.StartByte || n.EndByte() != target.EndByte) {
				conflict = true
			}
			return
		}
		if typ == "identifier" && n.Text(source) == name {
			for p := n.Parent(); p != nil; p = p.Parent() {
				switch p.Type(lang) {
				case "function_definition", "function_declaration":
					if id := p.ChildByFieldName("name", lang); id != nil && id.StartByte() == n.StartByte() {
						if p.StartByte() != target.StartByte || p.EndByte() != target.EndByte {
							conflict = true
						}
					}
					return
				case "arrow_function":
					if parameter := p.ChildByFieldName("parameter", lang); parameter != nil && parameter.StartByte() == n.StartByte() {
						conflict = true
					}
					return
				case "parameters", "formal_parameters", "lambda_parameters", "import_statement", "import_from_statement",
					"global_statement", "nonlocal_statement", "named_expression", "for_in_statement", "for_statement",
					"with_item", "except_clause", "delete_statement", "catch_clause", "list_comprehension", "dictionary_comprehension", "set_comprehension", "generator_expression":
					conflict = true
					return
				case "variable_declarator", "assignment", "augmented_assignment", "assignment_expression", "augmented_assignment_expression":
					for _, field := range []string{"name", "left"} {
						if binding := p.ChildByFieldName(field, lang); binding != nil && binding.StartByte() <= n.StartByte() && binding.EndByte() >= n.EndByte() {
							conflict = true
						}
					}
					return
				case "call", "call_expression", "expression_statement", "return_statement", "block", "statement_block":
					return
				}
			}
		}
		for i := 0; i < n.NamedChildCount(); i++ {
			visit(n.NamedChild(i))
		}
	}
	visit(tree.RootNode())
	return conflict
}
