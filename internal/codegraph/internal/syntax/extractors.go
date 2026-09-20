package syntax

import (
	gs "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

type programKey struct {
	language *gs.Language
	kinds    gs.FactKind
}

// extractFacts shares one traversal for imports and requested calls. Programs
// are bound to a grammar instance, not just its name; no syntax tree is retained.
func (a *Analyzer) extractFacts(tree *gs.Tree, calls bool) (gs.FactSet, error) {
	kinds := gs.FactImports
	if calls {
		kinds |= gs.FactCalls
	}
	key := programKey{tree.Language(), kinds}
	program := a.programs[key]
	if program == nil {
		var err error
		program, err = gs.NewFactProgram(key.language, kinds)
		if err != nil {
			return gs.FactSet{}, err
		}
		if a.programs == nil {
			a.programs = map[programKey]*gs.FactProgram{}
		}
		a.programs[key] = program
	}
	return program.Extract(tree), nil
}

func (a *Analyzer) outliner(lang *gs.Language, entry grammars.LangEntry) (*gs.Outliner, error) {
	if outliner := a.outliners[lang]; outliner != nil {
		return outliner, nil
	}
	outliner, err := gs.NewOutliner(lang, outlineQuery(entry),
		gs.WithOutlineOwnerRules(grammars.OutlineOwnerRules(entry)))
	if err != nil {
		return nil, err
	}
	if a.outliners == nil {
		a.outliners = map[*gs.Language]*gs.Outliner{}
	}
	a.outliners[lang] = outliner
	return outliner, nil
}

// +why=`An omission-free outline only covers declaration kinds present in its query`
// Preserve upstream ownership and ambiguity handling while adding declarations
// absent from its tags. Multiple names sharing a declaration span stay ambiguous.
func outlineQuery(entry grammars.LangEntry) string {
	query := grammars.ResolveTagsQuery(entry)
	switch entry.Language().Name {
	case "go":
		query += `
(type_spec name: (type_identifier) @name) @definition.type
(type_alias name: (type_identifier) @name) @definition.type
((const_spec name: (identifier) @name) @definition.constant
 (#not-eq? @name "_"))
((var_spec name: (identifier) @name) @definition.variable
 (#not-eq? @name "_"))`
	case "javascript", "typescript", "tsx":
		query += "\n(variable_declarator name: (identifier) @name) @definition.variable"
		if entry.Language().Name != "javascript" {
			query += "\n(type_alias_declaration name: (type_identifier) @name) @definition.type"
		}
	}
	return query
}
