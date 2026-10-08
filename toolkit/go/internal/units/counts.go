package units

import "encoding/json"

// ElementCounts describes changed source elements, separately for each version.
// Missing map entries read as zero; an element changed on both sides is not
// counted twice in one map. Unknown/whitespace count unowned fragments.
type ElementCounts struct {
	Before map[Kind]int `json:"before"`
	After  map[Kind]int `json:"after"`
}

// Only reports whether every counted element has one of the requested kinds.
// Empty counts do not prove an import-only (or any other kind-only) target.
func (c ElementCounts) Only(kinds ...Kind) bool {
	found := false
	for _, counts := range []map[Kind]int{c.Before, c.After} {
		for kind, n := range counts {
			if n == 0 {
				continue
			}
			found = true
			allowed := false
			for _, k := range kinds {
				if k == kind {
					allowed = true
					break
				}
			}
			if !allowed {
				return false
			}
		}
	}
	return found
}

// Counts returns the same element-count model used by Unit.Counts.
func (f Fragment) Counts() ElementCounts { return countElements([]Fragment{f}) }

func countElements(fs []Fragment) ElementCounts {
	c := ElementCounts{Before: map[Kind]int{}, After: map[Kind]int{}}
	for side, counts := range []map[Kind]int{c.Before, c.After} {
		seen := map[string]bool{}
		seenFragments := map[string]bool{}
		for _, f := range fs {
			id := FragmentID(f)
			if seenFragments[id] {
				continue
			}
			seenFragments[id] = true
			es := f.After
			if side == 0 {
				es = f.Before
			}
			for _, e := range es {
				key := elementID(e)
				if !seen[key] {
					counts[e.Kind]++
					seen[key] = true
				}
			}
			if len(es) == 0 && (len(f.ChangedSpans(side == 0)) > 0 || len(f.Before) == 0 && len(f.After) == 0 && side == 1 && len(ParseHunks(f.Diff)) == 0) {
				kind := Unknown
				if blankOnly(f) {
					kind = Whitespace
				}
				counts[kind]++
			}
		}
	}
	return c
}

func (f Fragment) MarshalJSON() ([]byte, error) {
	type fields Fragment
	return json.Marshal(struct {
		fields
		Counts ElementCounts `json:"counts"`
	}{fields(f), f.Counts()})
}
