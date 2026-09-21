package syntax

import (
	gs "github.com/odvcencio/gotreesitter"
)

type programKey struct {
	language *gs.Language
	kinds    gs.FactKind
}

// extractFacts extracts only repository-resolution facts. Declarations and
// calls are shared CodeGraph facts and are never reconstructed here.
func (a *Analyzer) extractFacts(tree *gs.Tree) (gs.FactSet, error) {
	key := programKey{tree.Language(), gs.FactImports}
	program := a.programs[key]
	if program == nil {
		var err error
		program, err = gs.NewFactProgram(key.language, key.kinds)
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
