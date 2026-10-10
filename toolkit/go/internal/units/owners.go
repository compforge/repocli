package units

import (
	"context"
	"slices"
	"sort"
)

// mergeOwners reunites edits to the same versioned declaration without changing
// Fragment identity. A pure addition and a replacement can have different before
// owners but the same after owner; graph self-edges cannot express this relation.
func mergeOwners(ctx context.Context, groups [][]string, fs map[string]Fragment, fits func([]string) bool) ([][]string, int, []Merge, error) {
	type owner struct {
		before            bool
		snapshot, element string
	}
	owners := map[owner][]string{}
	var keys []owner
	for _, group := range groups {
		for _, id := range group {
			f := fs[id]
			for side, elements := range [][]Element{f.Before, f.After} {
				snapshot := f.AfterRef
				if side == 0 {
					snapshot = f.BeforeRef
				}
				for _, e := range elements {
					if e.Kind == Import || e.Kind == Unknown || e.Kind == Whitespace {
						continue
					}
					key := owner{side == 0, snapshot, elementID(e)}
					if _, ok := owners[key]; !ok {
						keys = append(keys, key)
					}
					owners[key] = append(owners[key], id)
				}
			}
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.before != b.before {
			return a.before
		}
		if a.snapshot != b.snapshot {
			return a.snapshot < b.snapshot
		}
		return a.element < b.element
	})
	var decisions []Merge
	blocked := 0
	for _, key := range keys {
		ids := owners[key]
		for i := 0; i < len(groups); i++ {
			if err := ctx.Err(); err != nil {
				return nil, 0, nil, err
			}
			touches := func(g []string) bool {
				return slices.ContainsFunc(g, func(id string) bool { return slices.Contains(ids, id) })
			}
			if !touches(groups[i]) {
				continue
			}
			for j := i + 1; j < len(groups); {
				if !touches(groups[j]) {
					j++
					continue
				}
				combined := union(groups[i], groups[j])
				if !fits(combined) {
					blocked++
					j++
					continue
				}
				decisions = append(decisions, Merge{Strategy: "same_owner", FragmentIDs: combined})
				groups[i] = combined
				groups = append(groups[:j], groups[j+1:]...)
			}
		}
	}
	return groups, blocked, decisions, nil
}
