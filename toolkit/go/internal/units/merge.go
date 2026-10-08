package units

import (
	"context"
	"slices"
	"sort"
	"strings"

	cg "github.com/compforge/codegraph"
)

// Merge records a grouping decision; graph evidence remains in Relations.
type Merge struct {
	Strategy    string   `json:"strategy"`
	FragmentIDs []string `json:"fragment_ids"`
}

func relationStrength(e Relation, fs map[string]Fragment) int {
	if e.Link.Kind == cg.Encloses || e.Link.Kind == cg.Contains {
		return 0
	}
	if importOnly(fs[e.FromFragment]) || importOnly(fs[e.ToFragment]) {
		return 2
	}
	return 1
}

// mergeEdges never splits an existing Unit. A positive target stops immediately
// after a successful merge; zero exhausts the supported semantic relationships.
func mergeEdges(ctx context.Context, groups [][]string, edges []Relation, fs map[string]Fragment, target int, fits func([]string) bool, accept func(Relation) bool) ([][]string, int, []Merge, error) {
	blocked := 0
	var decisions []Merge
	for _, e := range edges {
		if !accept(e) {
			continue
		}
		for {
			if err := ctx.Err(); err != nil {
				return nil, 0, nil, err
			}
			if target > 0 && len(groups) <= target {
				return groups, blocked, decisions, nil
			}
			merged := false
		pairs:
			for i := 0; i < len(groups); i++ {
				for j := i + 1; j < len(groups); j++ {
					related := slices.Contains(groups[i], e.FromFragment) && slices.Contains(groups[j], e.ToFragment) || slices.Contains(groups[j], e.FromFragment) && slices.Contains(groups[i], e.ToFragment)
					if !related {
						continue
					}
					ids := union(groups[i], groups[j])
					if !fits(ids) {
						blocked++
						continue
					}
					reason := "calls"
					if relationStrength(e, fs) == 0 {
						reason = "contains"
					}
					decisions = append(decisions, Merge{Strategy: reason, FragmentIDs: ids})
					groups[i] = ids
					groups = append(groups[:j], groups[j+1:]...)
					merged = true
					break pairs
				}
			}
			if !merged {
				break
			}
		}
	}
	return groups, blocked, decisions, nil
}

func coalesceFiles(ctx context.Context, groups [][]string, fs map[string]Fragment, target int, fits func([]string) bool) ([][]string, int, []Merge, error) {
	blocked := 0
	var decisions []Merge
	for len(groups) > target {
		merged := false
		for i := 0; i < len(groups) && !merged; i++ {
			if err := ctx.Err(); err != nil {
				return nil, 0, nil, err
			}
			for j := i + 1; j < len(groups); j++ {
				same := false
				for _, a := range groups[i] {
					for _, b := range groups[j] {
						if fs[a].Path == fs[b].Path {
							same = true
						}
					}
				}
				if !same {
					continue
				}
				ids := union(groups[i], groups[j])
				if !fits(ids) {
					blocked++
					continue
				}
				decisions = append(decisions, Merge{Strategy: "file", FragmentIDs: ids})
				groups[i] = ids
				groups = append(groups[:j], groups[j+1:]...)
				merged = true
				break
			}
		}
		if !merged {
			break
		}
	}
	return groups, blocked, decisions, nil
}

func union(a, b []string) []string { return uniqueIDs(append(append([]string(nil), a...), b...)) }
func canonicalize(groups [][]string) {
	for _, g := range groups {
		sort.Strings(g)
	}
	sort.Slice(groups, func(i, j int) bool { return strings.Join(groups[i], "\x00") < strings.Join(groups[j], "\x00") })
}
