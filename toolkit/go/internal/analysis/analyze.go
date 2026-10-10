// Package analysis coordinates repository snapshots, change impact, and ownership.
// It does not depend on the command-line framework.
package analysis

import (
	"context"
	"io"
	"strings"

	cg "github.com/compforge/codegraph"
	"github.com/compforge/go-stdx/timeline"
	"github.com/compforge/quality-harness/sdks/go/common"
	"github.com/compforge/repocli/toolkit/go/internal/diff"
	"github.com/compforge/repocli/toolkit/go/internal/impact"
	"github.com/compforge/repocli/toolkit/go/internal/observation"
	"github.com/compforge/repocli/toolkit/go/internal/project"
)

type Request struct {
	// TagRules classifies Diff changes; nil uses CodeGraph builtins, an explicit
	// empty slice disables tags. It does not change AnalyzeImpact selection.
	TagRules     []cg.TagRule
	Repository   string
	Head         string
	Staged       bool
	ChangedFiles []string
	EmptyBase    bool // explicitly compare against an empty tree (root commits or unborn workspaces)
	Base         string
	PatchFile    string
	TestDirs     []string
	Stdin        io.Reader
	// MaxFiles bounds snapshot capture and impact parsing per version. Zero uses 10000.
	MaxFiles int
	// MaxSnapshotBytes bounds captured source bytes per version; zero uses 128 MiB.
	MaxSnapshotBytes int64
	// MaxNodes bounds each impact source graph. Zero uses 250000.
	MaxNodes int
	// MaxRelations bounds each impact source graph. Zero uses 500000.
	MaxRelations int
}

type Diagnostic struct {
	Reason          string   `json:"reason,omitempty"`
	Relation        string   `json:"relation,omitempty"`
	Confidence      string   `json:"confidence,omitempty"`
	Version         string   `json:"version,omitempty"`
	Line            int      `json:"line,omitempty"`
	PossibleTargets []string `json:"possibleTargets,omitempty"`
	Code            string   `json:"code"`
	Path            string   `json:"path,omitempty"`
	Message         string   `json:"message"`
}

type Report struct {
	impact.Result
	Head        string                    `json:"head,omitempty"`
	Snapshot    string                    `json:"snapshot"`
	Complete    bool                      `json:"complete"`
	Diagnostics []Diagnostic              `json:"diagnostics"`
	Checkout    string                    `json:"checkout"`
	Repository  *common.Repository        `json:"repository"`
	Base        string                    `json:"base"`
	Input       string                    `json:"input"`
	Components  []project.ComponentImpact `json:"components"`
}

// Analyze compares repository snapshots and attaches source ownership.
func Analyze(ctx context.Context, req Request) (report Report, err error) {
	if err := req.Validate(); err != nil {
		return Report{}, err
	}
	req.TestDirs, _ = impact.ValidateDirs(req.TestDirs)
	if req.Base == "" && !req.EmptyBase {
		req.Base = "HEAD"
	}
	stageRef, _ := timeline.StageFromContext(ctx)
	parentCtx := ctx
	ctx, stage := observation.Begin(parentCtx, stageRef.TimelineID, "analysis.snapshot", timeline.WithParent(stageRef.StageID))
	// End whichever stage is active on every return, retaining its real error.
	defer func() { stage.End(err) }()
	c, err := captureComparison(ctx, req)
	if err != nil {
		return Report{}, err
	}
	r, ref, head, input := c.repo, c.base, c.head, c.input
	before, after, changes := c.before, c.after, c.changes
	issues, skipped, gitlinks := c.issues, c.skipped, c.gitlinks
	origin, err := r.Origin(ctx)
	if err != nil {
		return Report{}, err
	}
	oldLayout, err := project.Load(before.Files, origin)
	if err != nil {
		return Report{}, err
	}
	newLayout, err := project.Load(after.Files, origin)
	if err != nil {
		return Report{}, err
	}
	stage.End(nil)
	ctx, stage = observation.Begin(parentCtx, stageRef.TimelineID, "analysis.impact", timeline.WithParent(stageRef.StageID))
	result, err := impact.Analyze(ctx, impact.Request{Before: before.Files, After: after.Files, BeforeResources: before.Resources, AfterResources: after.Resources, Changes: changes, TestDirs: req.TestDirs, Issues: issues, Skipped: skipped, Gitlinks: gitlinks, OldLayout: oldLayout, NewLayout: newLayout, MaxFiles: req.MaxFiles, MaxNodes: req.MaxNodes, MaxRelations: req.MaxRelations})
	if err != nil {
		return Report{}, err
	}
	stage.End(nil)
	ctx, stage = observation.Begin(parentCtx, stageRef.TimelineID, "analysis.finalize", timeline.WithParent(stageRef.StageID))
	diagnostics := []Diagnostic{}
	for _, message := range issues {
		diagnostics = append(diagnostics, diagnostic("snapshot_incomplete", message))
	}
	for _, issue := range result.Uncertainties {
		diagnostics = append(diagnostics, Diagnostic{Code: "impact_uncertain", Path: issue.Path, Message: issue.Message,
			Reason: issue.Reason, Relation: string(issue.Relation), Confidence: string(issue.Confidence), Version: issue.Version,
			Line: issue.Line, PossibleTargets: issue.PossibleTargets})
	}
	changed, err := c.changed(ctx, req)
	if err != nil {
		return Report{}, err
	}
	if changed {
		diagnostics = append(diagnostics, Diagnostic{Code: "snapshot_changed", Message: "repository contents changed during analysis"})
		result.Scope = "partial"
	}

	report = Report{Head: head, Snapshot: after.Digest(), Complete: len(diagnostics) == 0, Diagnostics: diagnostics, Result: result, Checkout: r.Root, Repository: newLayout.Repository, Base: ref, Input: input,
		Components: componentResults(oldLayout, newLayout, changes, result, diagnostics)}
	return report, nil
}

// Snapshot and impact adapters prefix file-local issues with their repository path.
// Preserve the human message while exposing that location to structured consumers.
func diagnostic(code, message string) Diagnostic {
	d := Diagnostic{Code: code, Message: message}
	if name, _, ok := strings.Cut(message, ": "); ok && diff.ValidPath(name) {
		d.Path = name
	}
	return d
}
