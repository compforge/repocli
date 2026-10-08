// Package analysis coordinates repository snapshots, change impact, and ownership.
// It does not depend on the command-line framework.
package analysis

import (
	"context"
	"io"
	"strings"

	"github.com/compforge/go-stdx/timeline"
	"github.com/compforge/quality-harness/sdks/go/common"
	"github.com/compforge/repocli/toolkit/go/internal/diff"
	"github.com/compforge/repocli/toolkit/go/internal/impact"
	"github.com/compforge/repocli/toolkit/go/internal/project"
)

type Request struct {
	Repository   string
	Head         string
	Staged       bool
	ChangedFiles []string
	EmptyBase    bool // explicitly compare against an empty tree (root commits or unborn workspaces)
	Base         string
	PatchFile    string
	TestDirs     []string
	Stdin        io.Reader
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
	operation, ok := timeline.FromContext(ctx)
	if !ok {
		operation = timeline.Noop("")
	}
	parentCtx := ctx
	ctx, stage := timeline.BeginContext(parentCtx, operation, "analysis.snapshot")
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
	ctx, stage = timeline.BeginContext(parentCtx, operation, "analysis.impact")
	result, err := impact.Analyze(ctx, impact.Request{Before: before.Files, After: after.Files, BeforeResources: before.Resources, AfterResources: after.Resources, Changes: changes, TestDirs: req.TestDirs, Issues: issues, Skipped: skipped, Gitlinks: gitlinks, OldLayout: oldLayout, NewLayout: newLayout})
	if err != nil {
		return Report{}, err
	}
	stage.End(nil)
	ctx, stage = timeline.BeginContext(parentCtx, operation, "analysis.finalize")
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
