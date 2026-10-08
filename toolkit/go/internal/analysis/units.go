package analysis

import (
	"context"
	"fmt"
	cg "github.com/compforge/codegraph"
	"github.com/compforge/go-stdx/timeline"
	"github.com/compforge/repocli/toolkit/go/internal/codegraph"
	"github.com/compforge/repocli/toolkit/go/internal/git"
	"github.com/compforge/repocli/toolkit/go/internal/units"
)

// UnitReport is the inspectable diff -> Fragment -> Unit result, independent of
// impact/test selection and any consumer's execution state.
type UnitReport struct {
	units.Result
	Schema         int            `json:"schema"`
	Checkout       string         `json:"checkout"`
	Base           string         `json:"base"`
	Head           string         `json:"head,omitempty"`
	Input          string         `json:"input"`
	BeforeSnapshot string         `json:"before_snapshot"`
	AfterSnapshot  string         `json:"after_snapshot"`
	Changes        []units.Change `json:"changes"`
}

func AnalyzeUnits(ctx context.Context, req Request, opts units.Options) (report UnitReport, err error) {
	if len(req.TestDirs) > 0 {
		return report, fmt.Errorf("unit analysis does not select tests")
	}
	op, ok := timeline.FromContext(ctx)
	if !ok {
		op = timeline.Noop("")
	}
	parent := ctx
	ctx, stage := timeline.BeginContext(parent, op, "units.comparison")
	defer func() { stage.End(err) }()
	c, err := captureComparison(ctx, req)
	if err != nil {
		return report, err
	}
	report = UnitReport{Schema: 1, Checkout: c.repo.Root, Base: c.base, Head: c.head, Input: c.input, BeforeSnapshot: c.before.Digest(), AfterSnapshot: c.after.Digest(), Changes: []units.Change{}}
	var oldPaths, newPaths []string
	for _, d := range c.changes {
		oldPath := d.OldPath
		if oldPath == "" {
			oldPath = d.Path
		}
		old, known := c.before.Files[oldPath]
		next, exists := c.after.Files[d.Path]
		ch := units.Change{OldPath: oldPath, NewPath: d.Path, Diff: d.Text(next), OldFileContent: string(old), OldContentKnown: known, NewFileContent: string(next), NewContentMissing: !exists && d.Status != "deleted", BeforeRef: report.BeforeSnapshot, AfterRef: report.AfterSnapshot, IsNew: d.Status == "added", IsDeleted: d.Status == "deleted", IsRenamed: d.Status == "renamed", IsBinary: d.Binary}
		if ch.IsDeleted {
			ch.NewPath = "/dev/null"
		}
		if ch.IsNew {
			ch.OldPath = "/dev/null"
		}
		for _, h := range d.Hunks {
			ch.Insertions += int64(h.New.Count)
			ch.Deletions += int64(h.Old.Count)
		}
		report.Changes = append(report.Changes, ch)
		if known && !ch.IsBinary {
			oldPaths = append(oldPaths, oldPath)
		}
		if exists && !ch.IsBinary {
			newPaths = append(newPaths, d.Path)
		}
	}
	stage.End(nil)
	ctx, stage = timeline.BeginContext(parent, op, "units.fragments")
	var fragments []units.Fragment
	for _, ch := range report.Changes {
		fs, e := units.Split(ctx, ch)
		if e != nil {
			return report, e
		}
		fragments = append(fragments, fs...)
	}
	stage.End(nil)
	ctx, stage = timeline.BeginContext(parent, op, "units.graphs")
	var diagnostics []units.Diagnostic
	cache, err := cg.NewExtractionCache(512, 128<<20)
	if err != nil {
		return report, err
	}
	build := func(s git.Snapshot, id string, paths []string) (*cg.Graph, error) {
		b, e := codegraph.NewBuilder(ctx, codegraph.BuildOptions{Snapshot: id, Files: s.Files, Resources: s.Resources, Gitlinks: c.gitlinks, Kinds: []codegraph.Kind{codegraph.Imports, codegraph.Calls, codegraph.Contains}, MaxDepth: 3, MaxFiles: 512, ExtractionCache: cache})
		if e != nil {
			return nil, e
		}
		if e = b.Add(ctx, paths...); e != nil {
			return nil, e
		}
		for _, d := range b.Result().Diagnostics {
			diagnostics = append(diagnostics, units.Diagnostic{Code: d.Code, Path: d.Path, Snapshot: id, Message: d.Message})
		}
		return b.SourceGraph(), nil
	}
	before, err := build(c.before, report.BeforeSnapshot, oldPaths)
	if err != nil {
		return report, fmt.Errorf("before graph: %w", err)
	}
	after, err := build(c.after, report.AfterSnapshot, newPaths)
	if err != nil {
		return report, fmt.Errorf("after graph: %w", err)
	}
	stage.End(nil)
	ctx, stage = timeline.BeginContext(parent, op, "units.formation")
	result, err := units.Group(ctx, fragments, before, after, opts)
	if err != nil {
		return report, err
	}
	for _, issue := range c.issues {
		diagnostics = append(diagnostics, units.Diagnostic{Code: "snapshot_incomplete", Message: issue})
	}
	for _, ch := range report.Changes {
		if reason := c.skipped[ch.Path()]; reason != "" {
			diagnostics = append(diagnostics, units.Diagnostic{Code: "source_unavailable", Path: ch.Path(), Message: reason})
		}
	}
	changed, err := c.changed(ctx, req)
	if err != nil {
		return report, err
	}
	if changed {
		diagnostics = append(diagnostics, units.Diagnostic{Code: "snapshot_changed", Message: "repository contents changed during analysis"})
	}
	result.Diagnostics = append(result.Diagnostics, diagnostics...)
	result.Complete = result.Complete && len(diagnostics) == 0
	report.Result = result
	return report, nil
}
