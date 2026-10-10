package units

import (
	"context"
	"sort"
)

// coalesceFiles packs local context even below the count ceiling. Prefer nearby
// declarations over hash order when capacity cannot accommodate the whole file.
// These are file-locality decisions, not invented semantic dependencies.
func coalesceFiles(ctx context.Context, groups [][]string, fs map[string]Fragment, fits func([]string) bool) ([][]string, int, []Merge, error) {
	type candidate struct{ i, j, distance int }
	blocked := 0
	var decisions []Merge
	for {
		var candidates []candidate
		for i := 0; i < len(groups); i++ {
			if err := ctx.Err(); err != nil {
				return nil, 0, nil, err
			}
			for j := i + 1; j < len(groups); j++ {
				distance, same := groupFileDistance(groups[i], groups[j], fs)
				if same {
					candidates = append(candidates, candidate{i, j, distance})
				}
			}
		}
		sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].distance < candidates[j].distance })
		merged := false
		for _, c := range candidates {
			ids := union(groups[c.i], groups[c.j])
			if !fits(ids) {
				blocked++
				continue
			}
			decisions = append(decisions, Merge{Strategy: "file", FragmentIDs: ids})
			groups[c.i] = ids
			groups = append(groups[:c.j], groups[c.j+1:]...)
			merged = true
			break
		}
		if !merged {
			return groups, blocked, decisions, nil
		}
	}
}

func groupFileDistance(a, b []string, fs map[string]Fragment) (int, bool) {
	distance := int(^uint(0) >> 1)
	same := false
	for _, x := range a {
		for _, y := range b {
			f, g := fs[x], fs[y]
			if f.Path != g.Path {
				continue
			}
			same = true
			// Compare coordinates only within the same source side. Unbound material
			// still joins by file, but does not outrank a located pair of declarations.
			for side, left := range [][]Element{f.Before, f.After} {
				right := g.Before
				if side == 1 {
					right = g.After
				}
				for _, l := range left {
					for _, r := range right {
						distance = min(distance, max(0, l.Span.Start-r.Span.End, r.Span.Start-l.Span.End))
					}
				}
			}
		}
	}
	return distance, same
}
