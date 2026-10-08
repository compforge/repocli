package units

import (
	"context"
	"fmt"
	cg "github.com/compforge/codegraph"
	"slices"
	"sort"
	"strings"
)

// Group retains every input Fragment and allows evidenced shared dependencies.
// +spec=`Each edit has one Fragment identity; Units may share Fragment references and merged Units deduplicate them`
func Group(ctx context.Context, fragments []Fragment, before, after *cg.Graph, opts Options) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if opts.MaxUnits < 0 || opts.MaxFiles < 0 || opts.MaxChangedLines < 0 || opts.MaxDiffSize < 0 {
		return Result{}, fmt.Errorf("unit limits must not be negative")
	}
	if opts.MaxFiles == 0 {
		opts.MaxFiles = 5
	}
	if opts.MaxChangedLines == 0 {
		opts.MaxChangedLines = 300
	}
	if opts.MaxDiffSize == 0 {
		opts.MaxDiffSize = 32000
	}
	if opts.DiffSize == nil {
		opts.DiffSize = func(s string) int { return len(s) }
	}
	fs := append([]Fragment(nil), fragments...)
	byID := map[string]Fragment{}
	byFile := map[string][]string{}
	result := Result{Fragments: fs, Units: []Unit{}, Relations: []Relation{}, Steps: []Step{}, Diagnostics: []Diagnostic{}, Complete: true}
	for i := range fs {
		fs[i].ID = FragmentID(fs[i])
		f := fs[i]
		if _, ok := byID[f.ID]; ok {
			return Result{}, fmt.Errorf("repeated fragment %s", f.ID)
		}
		byID[f.ID] = f
		byFile[f.Path] = append(byFile[f.Path], f.ID)
		for _, gap := range f.Gaps {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "source_gap", Path: f.Path, Message: gap})
			result.Complete = false
		}
	}
	sort.Slice(fs, func(i, j int) bool { return fs[i].ID < fs[j].ID })
	for _, g := range []*cg.Graph{before, after} {
		if g != nil {
			for _, d := range g.Report().Diagnostics {
				result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: d.Code, Snapshot: g.Snapshot(), Message: d.Message})
				result.Complete = false
			}
		}
	}
	edges, err := relations(ctx, fs, before, after)
	if err != nil {
		return Result{}, err
	}
	result.Relations = edges
	size := func(ids []string) (int, int64, int) {
		files := map[string]bool{}
		var lines int64
		var patches []string
		for _, id := range ids {
			f := byID[id]
			files[f.Path] = true
			lines += f.Insertions + f.Deletions
			patch := f.Diff
			if len(ids) > 1 {
				patch = "// " + f.Path + "\n" + patch
			}
			patches = append(patches, patch)
		}
		return len(files), lines, opts.DiffSize(strings.Join(patches, "\n"))
	}
	fits := func(ids []string) bool {
		// Reject cheap limits before constructing a patch or invoking a caller's
		// potentially expensive tokenizer. Exact diff size is still used if eligible.
		files := map[string]bool{}
		var lines int64
		for _, id := range ids {
			f := byID[id]
			files[f.Path] = true
			lines += f.Insertions + f.Deletions
			if len(files) > opts.MaxFiles || lines > opts.MaxChangedLines {
				return false
			}
		}
		_, _, n := size(ids)
		return n <= opts.MaxDiffSize
	}
	paths := make([]string, 0, len(byFile))
	for p := range byFile {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var groups [][]string
	blocked := 0
	for _, p := range paths {
		ids := byFile[p]
		sort.Strings(ids)
		if opts.FileOnly {
			groups = append(groups, ids)
			continue
		}
		local, n, decisions, e := localGroups(ctx, ids, byID, edges, fits)
		if e != nil {
			return Result{}, e
		}
		groups = append(groups, local...)
		blocked += n
		result.Decisions = append(result.Decisions, decisions...)
	}
	canonicalize(groups)
	result.Steps = append(result.Steps, Step{Strategy: "local", InputUnits: len(fs), OutputUnits: len(groups), BudgetBlocked: blocked})
	initial := len(groups)
	var decisions []Merge
	if !opts.FileOnly && (opts.MaxUnits == 0 || len(groups) > opts.MaxUnits) {
		groups, blocked, decisions, err = mergeEdges(ctx, groups, edges, byID, opts.MaxUnits, fits, func(e Relation) bool {
			return byID[e.FromFragment].Path != byID[e.ToFragment].Path && relationStrength(e, byID) < 2
		})
		if err != nil {
			return Result{}, err
		}
		result.Decisions = append(result.Decisions, decisions...)
		result.Steps = append(result.Steps, Step{Strategy: "relations", InputUnits: initial, OutputUnits: len(groups), BudgetBlocked: blocked})
	}
	if opts.MaxUnits > 0 && len(groups) > opts.MaxUnits {
		initial = len(groups)
		groups, blocked, decisions, err = coalesceFiles(ctx, groups, byID, opts.MaxUnits, fits)
		if err != nil {
			return Result{}, err
		}
		result.Decisions = append(result.Decisions, decisions...)
		result.Steps = append(result.Steps, Step{Strategy: "file", InputUnits: initial, OutputUnits: len(groups), BudgetBlocked: blocked})
	}
	if opts.MaxUnits > 0 && len(groups) > opts.MaxUnits {
		initial = len(groups)
		groups, blocked, result.Merges, err = coalesce(ctx, groups, byID, before, after, opts.MaxUnits, fits)
		if err != nil {
			return Result{}, err
		}
		result.Steps = append(result.Steps, Step{Strategy: "namespace", InputUnits: initial, OutputUnits: len(groups), BudgetBlocked: blocked})
	}
	canonicalize(groups)
	for _, ids := range groups {
		_, _, n := size(ids)
		u := Unit{ID: stableID("unit", ids...), FragmentIDs: ids, DiffSize: n, BudgetExceeded: !fits(ids)}
		var members []Fragment
		for _, id := range ids {
			members = append(members, byID[id])
		}
		u.Counts = countElements(members)
		for _, id := range ids {
			u.Paths = append(u.Paths, byID[id].Path)
		}
		u.Paths = uniqueIDs(u.Paths)
		for _, e := range edges {
			a, b := slices.Contains(ids, e.FromFragment), slices.Contains(ids, e.ToFragment)
			if a && b {
				u.Relations = append(u.Relations, e)
			} else if a || b {
				u.Boundaries = append(u.Boundaries, e)
			}
		}
		result.Units = append(result.Units, u)
	}
	result.LimitExceeded = opts.MaxUnits > 0 && len(groups) > opts.MaxUnits
	return result, nil
}
