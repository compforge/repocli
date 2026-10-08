package units

import (
	"context"
	cg "github.com/compforge/codegraph"
	"slices"
	"sort"
)

// Connection is a graph-backed grouping candidate. Touched distinguishes an
// occurrence on changed lines from a declaration's existing dependency.
type Connection struct {
	Snapshot   string          `json:"snapshot"`
	Source     string          `json:"source"`
	Target     string          `json:"target"`
	SourcePath string          `json:"source_path,omitempty"`
	TargetPath string          `json:"target_path,omitempty"`
	Kind       cg.RelationKind `json:"kind"`
	Confidence cg.Confidence   `json:"confidence"`
	Touched    bool            `json:"touched"`
}

// relations keeps binding nodes as well as resolved declarations. This is why
// an import can travel with its users without guessing from identifier spelling.
func relations(ctx context.Context, fs []Fragment, graphs ...*cg.Graph) ([]Relation, error) {
	var out []Relation
	seen := map[Relation]bool{}
	for side, g := range graphs {
		if g == nil {
			continue
		}
		// RelationsFrom scans all graph edges. Index once for this grouping pass:
		// a repository-wide graph can contain many unrelated reference nodes.
		outgoing := map[string][]cg.Relation{}
		for _, e := range g.Relations() {
			outgoing[e.Source] = append(outgoing[e.Source], e)
		}
		from := func(id string, kinds ...cg.RelationKind) []cg.Relation {
			var edges []cg.Relation
			for _, e := range outgoing[id] {
				if slices.Contains(kinds, e.Kind) {
					edges = append(edges, e)
				}
			}
			return edges
		}
		owners := map[string][]int{}
		for i, f := range fs {
			ref := f.AfterRef
			if side == 0 {
				ref = f.BeforeRef
			}
			if ref != "" && ref != g.Snapshot() {
				return nil, &snapshotError{ref, g.Snapshot()}
			}
			for _, id := range fragmentNodes(f, g, side == 0) {
				owners[id] = append(owners[id], i)
			}
		}
		emit := func(source, target string, kind cg.RelationKind, confidence cg.Confidence, touched bool) {
			s, _ := g.Node(source)
			t, _ := g.Node(target)
			for _, i := range owners[source] {
				for _, j := range owners[target] {
					if i == j {
						continue
					}
					c := Connection{Snapshot: g.Snapshot(), Source: source, Target: target, Kind: kind, Confidence: confidence, Touched: touched}
					if s.Location != nil {
						c.SourcePath = s.Location.Path
					}
					if t.Location != nil {
						c.TargetPath = t.Location.Path
					}
					e := Relation{Before: side == 0, FromFragment: fs[i].ID, ToFragment: fs[j].ID, Link: c}
					if !seen[e] {
						seen[e] = true
						out = append(out, e)
					}
				}
			}
		}
		follow := func(source, target string, kind cg.RelationKind, confidence cg.Confidence, touched bool) {
			type hop struct {
				id string
				c  cg.Confidence
			}
			queue := []hop{{target, confidence}}
			visited := map[string]bool{}
			for len(queue) > 0 {
				h := queue[0]
				queue = queue[1:]
				if visited[h.id] {
					continue
				}
				visited[h.id] = true
				emit(source, h.id, kind, h.c, touched)
				for _, e := range from(h.id, cg.Aliases) {
					if e.Confidence.AtLeast(cg.Scoped) {
						queue = append(queue, hop{e.Target, h.c.Weaker(e.Confidence)})
					}
				}
			}
		}
		for id := range owners {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			for _, e := range from(id, cg.Calls, cg.References, cg.Extends, cg.Implements, cg.Aliases, cg.Exports, cg.Encloses, cg.Contains) {
				if e.Confidence.AtLeast(cg.Scoped) {
					follow(id, e.Target, e.Kind, e.Confidence, false)
				}
			}
		}
		for _, n := range g.Nodes() {
			if n.Kind != cg.Reference || n.Location == nil {
				continue
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			for _, owner := range from(n.ID, cg.OccursIn) {
				if len(owners[owner.Target]) == 0 {
					continue
				}
				touched := false
				for _, i := range owners[owner.Target] {
					for _, span := range fs[i].ChangedSpans(side == 0) {
						if n.Location.Line <= span.End && locationSpan(*n.Location).End >= span.Start {
							touched = true
						}
					}
				}
				for _, e := range from(n.ID, cg.References) {
					if e.Confidence.AtLeast(cg.Scoped) {
						follow(owner.Target, e.Target, cg.References, e.Confidence, touched)
					}
				}
			}
		}
	}
	byID := map[string]Fragment{}
	for _, f := range fs {
		byID[f.ID] = f
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if x, y := relationStrength(a, byID), relationStrength(b, byID); x != y {
			return x < y
		}
		if a.Link.Touched != b.Link.Touched {
			return a.Link.Touched
		}
		if a.Link.Confidence != b.Link.Confidence {
			return a.Link.Confidence.AtLeast(b.Link.Confidence)
		}
		if a.FromFragment != b.FromFragment {
			return a.FromFragment < b.FromFragment
		}
		if a.ToFragment != b.ToFragment {
			return a.ToFragment < b.ToFragment
		}
		if a.Before != b.Before {
			return a.Before
		}
		return a.Link.Kind < b.Link.Kind
	})
	return out, nil
}

type snapshotError struct{ anchor, graph string }

func (e *snapshotError) Error() string {
	return "fragment snapshot " + e.anchor + " does not match graph " + e.graph
}
