package units

import (
	"sort"
	"strings"
)

// splitChange partitions edits, not AST nodes. A replacement block can
// touch several owners; keeping that patch together does not assert that an old
// declaration and a new declaration have the same identity.
func splitChange(d Change, before, after []Element, gaps []string) []Fragment {
	var out []Fragment
	grouped := map[string]int{}
	for _, h := range ParseHunks(d.Diff) {
		oldLine, newLine := h.OldStart, h.NewStart
		for i := 0; i < len(h.Lines); {
			if h.Lines[i].Type == HunkContext {
				oldLine++
				newLine++
				i++
				continue
			}
			start := i
			oldStart, newStart := oldLine, newLine
			var oldOwners, newOwners []Element
			// A contiguous replacement is one patch. Pure additions/removals may be
			// split at ownership boundaries without inventing cross-version matches.
			end := i
			added, deleted := false, false
			for end < len(h.Lines) && h.Lines[end].Type != HunkContext {
				added = added || h.Lines[end].Type == HunkAdded
				deleted = deleted || h.Lines[end].Type == HunkDeleted
				end++
			}
			var owner string
			for i < end {
				l := h.Lines[i]
				anchors, line := after, newLine
				if l.Type == HunkDeleted {
					anchors, line = before, oldLine
				}
				a, ok := ElementAt(anchors, line)
				key := elementID(a)
				if i > start && !(added && deleted) && key != owner {
					break
				}
				owner = key
				if l.Type == HunkDeleted {
					if ok {
						oldOwners = appendElement(oldOwners, a)
					}
					oldLine++
				} else {
					if ok {
						newOwners = appendElement(newOwners, a)
					}
					newLine++
				}
				i++
			}
			part := Hunk{OldStart: oldStart, NewStart: newStart, OldCount: oldLine - oldStart, NewCount: newLine - newStart, Lines: append([]HunkLine(nil), h.Lines[start:i]...)}
			// Include only neighboring unchanged lines: changed lines retain one owner.
			for j := start - 1; j >= 0 && start-j <= 3 && h.Lines[j].Type == HunkContext; j-- {
				part.OldStart--
				part.NewStart--
				part.OldCount++
				part.NewCount++
				part.Lines = append([]HunkLine{h.Lines[j]}, part.Lines...)
			}
			for j := i; j < len(h.Lines) && j-i < 3 && h.Lines[j].Type == HunkContext; j++ {
				part.OldCount++
				part.NewCount++
				part.Lines = append(part.Lines, h.Lines[j])
			}
			sortElements(oldOwners)
			sortElements(newOwners)
			key := elementKey(oldOwners) + "|" + elementKey(newOwners)
			ins, del := countChanges([]Hunk{part})
			if index, ok := grouped[key]; ok && key != "|" {
				f := &out[index]
				f.Diff += renderHunks([]Hunk{part})
				f.Insertions += ins
				f.Deletions += del
			} else {
				f := Fragment{Path: d.Path(), OldPath: d.OldPath, Before: oldOwners, After: newOwners, Diff: diffHeader(d.Diff) + renderHunks([]Hunk{part}), Insertions: ins, Deletions: del, Gaps: gaps}
				if d.IsDeleted {
					f.Status = "deleted"
				} else if d.IsNew {
					f.Status = "added"
				} else if d.IsRenamed {
					f.Status = "renamed"
				}
				for _, a := range newOwners {
					if a.Name != "" {
						f.Symbols = append(f.Symbols, a.Name)
					}
				}
				grouped[key] = len(out)
				out = append(out, f)
			}
		}
	}
	if len(out) == 0 {
		out = []Fragment{{Path: d.Path(), OldPath: d.OldPath, Diff: d.Diff, Insertions: d.Insertions, Deletions: d.Deletions}}
		out[0].Gaps = gaps
		if d.IsDeleted {
			out[0].Status = "deleted"
		} else if d.IsNew {
			out[0].Status = "added"
		} else if d.IsRenamed {
			out[0].Status = "renamed"
		}
	}
	return out
}

func appendElement(as []Element, a Element) []Element {
	for _, x := range as {
		if elementID(x) == elementID(a) {
			return as
		}
	}
	return append(as, a)
}
func sortElements(as []Element) {
	sort.Slice(as, func(i, j int) bool { return elementID(as[i]) < elementID(as[j]) })
}
func elementKey(as []Element) string {
	var ids []string
	for _, a := range as {
		ids = append(ids, elementID(a))
	}
	return strings.Join(ids, "\x00")
}

// FragmentID is scoped to the exact patch and its source identities. Labels and
// short names must not join state belonging to different files or revisions.
func FragmentID(f Fragment) string {
	return stableID("fragment", f.Path, f.OldPath, f.BeforeRef, f.AfterRef, elementKey(f.Before), elementKey(f.After), f.Diff)
}
