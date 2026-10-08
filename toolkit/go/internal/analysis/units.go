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

// FormUnits consumes one captured diff. Filtering never discards the unchanged
// source needed for graph evidence, and this path never rereads the checkout.
func FormUnits(ctx context.Context, input DiffReport, opts units.Options) (report UnitReport, err error) {
	op, ok := timeline.FromContext(ctx)
	if !ok {
		op = timeline.Noop("")
	}
	ctx, stage := timeline.BeginContext(ctx, op, "units.formation")
	defer func() { stage.End(err) }()
	report = UnitReport{Schema: 1, Checkout: input.Checkout, Base: input.Base, Head: input.Head, Input: input.Input, BeforeSnapshot: input.BeforeSnapshot, AfterSnapshot: input.AfterSnapshot, Changes: input.Changes}
	before, after := input.beforeGraph, input.afterGraph
	var diagnostics []units.Diagnostic
	if input.captured != nil && before == nil && after == nil && len(input.Changes) > 0 && !opts.FileOnly {
		before, after, diagnostics, err = unitGraphs(ctx, input)
		if err != nil {
			return report, err
		}
	}
	result, err := units.Form(ctx, units.Input{Changes: input.Changes, Before: before, After: after, Options: opts})
	if err != nil {
		return report, err
	}
	result.Diagnostics = append(result.Diagnostics, input.Diagnostics...)
	result.Diagnostics = append(result.Diagnostics, diagnostics...)
	result.Complete = result.Complete && len(result.Diagnostics) == 0
	report.Result = result
	return report, nil
}

func unitGraphs(ctx context.Context, input DiffReport) (before, after *cg.Graph, diagnostics []units.Diagnostic, err error) {
	c := input.captured
	var oldPaths, newPaths []string
	for _, ch := range input.Changes {
		if ch.IsBinary {
			continue
		}
		if !ch.IsNew && ch.OldContentKnown {
			oldPaths = append(oldPaths, ch.OldPath)
		}
		if !ch.IsDeleted && !ch.NewContentMissing {
			newPaths = append(newPaths, ch.NewPath)
		}
	}
	cache, err := cg.NewExtractionCache(512, 128<<20)
	if err != nil {
		return nil, nil, nil, err
	}
	build := func(s git.Snapshot, id string, paths []string) (*cg.Graph, error) {
		b, e := codegraph.NewBuilder(ctx, codegraph.BuildOptions{TagRules: input.tagRules, Snapshot: id, Files: s.Files, Resources: s.Resources, Gitlinks: c.gitlinks, Kinds: []codegraph.Kind{codegraph.Imports, codegraph.Calls, codegraph.Contains}, MaxDepth: 3, MaxFiles: 512, ExtractionCache: cache})
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
	before, err = build(c.before, input.BeforeSnapshot, oldPaths)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("before graph: %w", err)
	}
	after, err = build(c.after, input.AfterSnapshot, newPaths)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("after graph: %w", err)
	}

	return before, after, diagnostics, nil
}
