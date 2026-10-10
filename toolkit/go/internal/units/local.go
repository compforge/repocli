package units

import (
	"context"
	"slices"
	"strings"
)

func importOnly(f Fragment) bool { return f.Counts().Only(Import) }
func blankOnly(f Fragment) bool {
	changed := false
	for _, h := range ParseHunks(f.Diff) {
		for _, l := range h.Lines {
			if l.Type == HunkContext {
				continue
			}
			changed = true
			if strings.TrimSpace(l.Content) != "" {
				return false
			}
		}
	}
	return changed
}

// localGroups establishes semantic Units independently of the count target.
// Unknown fragments coalesce per file; imports attach only through graph evidence.
func localGroups(ctx context.Context, ids []string, fs map[string]Fragment, edges []Relation, fits func([]string) bool) ([][]string, int, []Merge, error) {
	var groups [][]string
	var imports, unknown []string
	for _, id := range ids {
		counts := fs[id].Counts()
		switch {
		case counts.Only(Import):
			imports = append(imports, id)
		case counts.Only(Unknown, Whitespace):
			unknown = append(unknown, id)
		default:
			groups = append(groups, []string{id})
		}
	}
	groups, blocked, decisions, err := mergeOwners(ctx, groups, fs, fits)
	if err != nil {
		return nil, 0, nil, err
	}
	groups, n, related, err := mergeEdges(ctx, groups, edges, fs, fits, func(e Relation) bool {
		return fs[e.FromFragment].Path == fs[e.ToFragment].Path && relationStrength(e, fs) < 2
	})
	if err != nil {
		return nil, 0, nil, err
	}
	blocked += n
	decisions = append(decisions, related...)
	var unattached []string
	for _, id := range imports {
		attached := false
		for i, g := range groups {
			for _, e := range edges {
				if e.ToFragment != id || !slices.Contains(g, e.FromFragment) {
					continue
				}
				combined := union(groups[i], []string{id})
				if !fits(combined) {
					blocked++
					continue
				}
				groups[i] = combined
				attached = true
				decisions = append(decisions, Merge{Strategy: "import", FragmentIDs: combined})
				break
			}
		}
		if !attached {
			unattached = append(unattached, id)
		}
	}
	// Grouping unbound fragments by category is a file-local fallback, not an
	// invented dependency. Later file coalescing can reunite these Units with users.
	for _, remaining := range [][]string{unattached, unknown} {
		if len(remaining) > 0 {
			groups = append(groups, remaining)
			if len(remaining) > 1 {
				decisions = append(decisions, Merge{Strategy: "unbound", FragmentIDs: remaining})
			}
		}
	}
	return groups, blocked, decisions, nil
}
