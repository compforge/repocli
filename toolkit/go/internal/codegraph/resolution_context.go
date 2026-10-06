package codegraph

import shared "github.com/compforge/codegraph"

// Discovery owns repository configuration and module candidates; CodeGraph
// owns lexical binding and resolution against the final supplied workset.
func (b *Builder) addResolutions(name, language string, resolved []resolvedImport) {
	// Go uses the module-root map in every phase.
	if language == "go" {
		return
	}
	type key struct {
		start               int
		path, from, binding string
	}
	positions := map[key]int{}
	var inputs []shared.ImportResolution
	for _, resolution := range resolved {
		targets := resolution.targets
		if language == "python" {
			targets = resolution.modules
		}
		confidence := shared.Exact
		switch resolution.confidence {
		case Strong, Scoped:
			confidence = shared.Scoped
		case Weak, NameOnly:
			confidence = shared.NameOnly
		case Heuristic:
			confidence = shared.Heuristic
		}
		imp := resolution.reference
		// Dynamic imports are consumer exploration evidence, not lexical imports.
		if imp.Location.Path == "" {
			continue
		}
		id := key{imp.Location.StartByte, imp.Path, imp.From, imp.Binding}
		if index, ok := positions[id]; ok {
			// Bounded Python interpretation may visit one lexical import in several
			// loop iterations. Preserve candidate union and the weakest context.
			input := &inputs[index]
			input.Targets = unique(append(input.Targets, targets...))
			input.Confidence = input.Confidence.Weaker(confidence)
			continue
		}
		positions[id] = len(inputs)
		inputs = append(inputs, shared.ImportResolution{Document: name, Import: imp, Targets: targets, Confidence: confidence, Basis: resolution.basis})
	}
	for _, input := range inputs {
		// Catalog-only name matches drive impact exploration, not lexical binding.
		// Forward only repository path/configuration evidence; otherwise CodeGraph
		// keeps its independent language rules and local unresolved diagnostics.
		if !input.Confidence.AtLeast(shared.Scoped) {
			continue
		}
		b.resolution.Imports = append(b.resolution.Imports, input)
	}
}
