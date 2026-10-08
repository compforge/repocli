package units

import cg "github.com/compforge/codegraph"

// fragmentNodes is a grouping-time association, never stored on Fragment.
// Names alone cannot bind two source regions. Matching uses the same version,
// file and declaration span. Unknown edits stay unbound.
func fragmentNodes(f Fragment, g *cg.Graph, before bool) []string {
	elements, path := f.After, f.Path
	if before {
		elements, path = f.Before, f.OldPath
	}
	var ids []string
	for _, e := range elements {
		for _, n := range g.Nodes() {
			l := n.Location
			if l == nil || l.Path != path || n.Kind == cg.Reference || n.Kind == cg.DocumentNodeKind {
				continue
			}
			if l.StartByte >= e.StartByte && l.EndByte <= e.EndByte && (e.Kind != Import || n.Kind == cg.Import) {
				// Nested declarations belong to their own fragment when they changed.
				// Bind the matching named declaration rather than every contained node.
				if e.Name != "" && n.QualifiedName != e.Name && n.Name != e.Name {
					continue
				}
				ids = append(ids, n.ID)
			}
		}
	}
	return uniqueIDs(ids)
}
