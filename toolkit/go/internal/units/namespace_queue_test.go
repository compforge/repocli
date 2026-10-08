package units

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	cg "github.com/compforge/codegraph"
)

func TestNamespaceQueuePreservesExhaustiveMergeOrder(t *testing.T) {
	root := namespaceKey{"new", "root"}
	p, q := namespaceKey{"new", "p"}, namespaceKey{"new", "q"}
	heights := map[namespaceKey]int{root: 0, p: 1, q: 1}
	scopes := map[string]map[namespaceKey]cg.NamespaceMatch{}
	for i, id := range []string{"a", "b", "c", "d", "e", "f", "g", "shared"} {
		leaf := p
		if i%2 == 1 {
			leaf = q
		}
		scopes[id] = map[namespaceKey]cg.NamespaceMatch{root: {Depth: 2 + i%2, Paths: []cg.Path{{Nodes: []cg.Node{{ID: id}, {ID: root.id}}}}}, leaf: {Depth: 1, Paths: []cg.Path{{Nodes: []cg.Node{{ID: id}, {ID: leaf.id}}}}}}
	}
	fixtures := [][][]string{
		{{"a"}, {"b"}, {"c"}, {"d"}, {"e"}, {"f"}},
		{{"a", "c"}, {"b"}, {"d", "f"}, {"e"}},
		{{"a", "shared"}, {"c", "shared"}, {"b"}, {"d"}, {"g"}},
		{{"a"}, {"unknown"}, {"c"}, {"d"}},
	}
	for fixture, groups := range fixtures {
		for _, budget := range []int{1, 2, 3, 8} {
			for limit := 1; limit < len(groups); limit++ {
				fits := func(ids []string) bool { return len(ids) <= budget }
				want, _, wantMerges, err := exhaustiveNamespaceGroups(context.Background(), append([][]string(nil), groups...), scopes, heights, limit, fits)
				if err != nil {
					t.Fatal(err)
				}
				got, _, gotMerges, err := mergeNamespaceGroups(context.Background(), groups, scopes, heights, limit, fits)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(gotMerges, wantMerges) {
					t.Fatalf("fixture=%d budget=%d limit=%d: groups=%v want=%v merges=%+v want=%+v", fixture, budget, limit, got, want, gotMerges, wantMerges)
				}
			}
		}
	}
}

func TestNamespaceQueueDoesNotReevaluateUnchangedCandidates(t *testing.T) {
	const n = 80
	root := namespaceKey{"new", "root"}
	scopes := map[string]map[namespaceKey]cg.NamespaceMatch{}
	var groups [][]string
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("f%03d", i)
		groups = append(groups, []string{id})
		scopes[id] = map[namespaceKey]cg.NamespaceMatch{root: {Depth: 1}}
	}
	calls := map[string]int{}
	fits := func(ids []string) bool {
		key := strings.Join(ids, ",")
		calls[key]++
		if calls[key] > 1 {
			t.Fatalf("unchanged candidate reevaluated: %s", key)
		}
		return len(ids) <= 5
	}
	got, blocked, merges, err := mergeNamespaceGroups(context.Background(), groups, scopes, map[namespaceKey]int{root: 0}, 1, fits)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != n/5 || len(merges) != n-n/5 || blocked == 0 {
		t.Fatalf("groups=%d merges=%d blocked=%d", len(got), len(merges), blocked)
	}
	if len(calls) > n*n {
		t.Fatalf("candidate evaluations=%d exceeds quadratic bound", len(calls))
	}
}

// Retain the original exhaustive policy as an oracle for ordering, budgets and
// shared fragments; production must not reintroduce its repeated full scans.
func exhaustiveNamespaceGroups(ctx context.Context, groups [][]string, scopes map[string]map[namespaceKey]cg.NamespaceMatch, heights map[namespaceKey]int, limit int, fits func([]string) bool) ([][]string, int, []NamespaceMerge, error) {
	common := func(ids []string) map[namespaceKey]cg.NamespaceMatch {
		out := map[namespaceKey]cg.NamespaceMatch{}
		for i, id := range ids {
			if i == 0 {
				for k, v := range scopes[id] {
					out[k] = v
				}
				continue
			}
			for k, v := range out {
				if next, ok := scopes[id][k]; ok {
					v.Depth = max(v.Depth, next.Depth)
					v.Paths = append(append([]cg.Path(nil), v.Paths...), next.Paths...)
					out[k] = v
				} else {
					delete(out, k)
				}
			}
		}
		return out
	}
	blocked := 0
	var merges []NamespaceMerge
	for len(groups) > limit {
		if err := ctx.Err(); err != nil {
			return nil, 0, nil, err
		}
		left, right, depth, height := -1, -1, 0, -1
		var best namespaceKey
		var proof cg.NamespaceMatch
		for i := range groups {
			for j := i + 1; j < len(groups); j++ {
				ids := union(groups[i], groups[j])
				matches := common(ids)
				if len(matches) == 0 {
					continue
				}
				if !fits(ids) {
					blocked++
					continue
				}
				keys := make([]namespaceKey, 0, len(matches))
				for k := range matches {
					keys = append(keys, k)
				}
				sort.Slice(keys, func(i, j int) bool {
					if keys[i].snapshot != keys[j].snapshot {
						return keys[i].snapshot < keys[j].snapshot
					}
					return keys[i].id < keys[j].id
				})
				for _, k := range keys {
					m := matches[k]
					if left < 0 || heights[k] > height || heights[k] == height && m.Depth < depth {
						height = heights[k]
						left, right, depth, best, proof = i, j, m.Depth, k, m
					}
				}
			}
		}
		if left < 0 {
			break
		}
		ids := union(groups[left], groups[right])
		merges = append(merges, NamespaceMerge{best.snapshot, best.id, ids, proof.Paths})
		groups[left] = ids
		groups = append(groups[:right], groups[right+1:]...)
	}
	return groups, blocked, merges, nil
}

func TestGroupingSkipsDiffSizeWhenFileBudgetFails(t *testing.T) {
	files := map[string]string{"go.mod": "module example\n", "a.go": "package p\nfunc A(){}\n", "b.go": "package p\nfunc B(){}\n"}
	r, err := Group(context.Background(), functionFragments(t, files), nil, graph(t, "new", files), Options{MaxUnits: 1, MaxFiles: 1, DiffSize: func(diff string) int {
		if strings.Contains(diff, "func A") && strings.Contains(diff, "func B") {
			t.Fatal("tokenized an already rejected multi-file candidate")
		}
		return len(diff)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Units) != 2 || !r.LimitExceeded {
		t.Fatalf("%+v", r)
	}
}
