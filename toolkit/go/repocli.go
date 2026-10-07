// Package repocli provides read-only repository analysis and explicit Git worktree operations. Applications, including the CLI, consume this module through its public API.
//
// Operations may invoke Git, but never the repocli executable or target-project
// commands. Callers own context deadlines, output, persistence and execution policy.
package repocli

import (
	"context"

	"github.com/compforge/repocli/toolkit/go/internal/analysis"
	"github.com/compforge/repocli/toolkit/go/internal/git"
	"github.com/compforge/repocli/toolkit/go/internal/impact"
	"github.com/compforge/repocli/toolkit/go/internal/project"
)

// InputRequest selects a working tree, index (Staged), or exact commit/ref (Head).
// Repository is a checkout path; an empty path selects the current directory.
type InputRequest = analysis.InputRequest

// InspectReport describes organization and observation completeness, not content identity.
type InspectReport = analysis.InspectReport

// Layout describes repository identity and component boundaries. Owner accepts a
// repository-relative path, including a deleted path, and returns its deepest component.
type Layout = project.Layout

// ComponentBinding extends common.Component with its observed root, products and tool evidence.
// Repository, Component and Product identities remain quality-harness common types.
type ComponentBinding = project.Binding

// PackageTool records a package-tool declaration or lockfile observation.
type PackageTool = project.PackageTool

// Diagnostic records an observed gap; a successful call need not be complete.
type Diagnostic = analysis.Diagnostic

// SnapshotReport identifies captured contents and their completeness.
type SnapshotReport = analysis.SnapshotReport

// DiffRequest selects a baseline and a working-tree, index, commit or patch comparison.
// Base defaults to HEAD. PatchFile "-" reads Stdin supplied by the caller.
type DiffRequest = analysis.Request

// DiffReport contains changes, impact evidence, component ownership and diagnostics.
// An empty test list is not proof that tests are independent of the change.
type DiffReport = analysis.Report

// Uncertainty describes a scoped static-analysis observation or coverage gap.
type Uncertainty = impact.Uncertainty

// ComponentImpact retains ownership and impact from each comparison version.
type ComponentImpact = project.ComponentImpact

// GraphSnapshot keeps nodes, relations and source bytes tied to one content version.
// Its data is caller-owned and should be treated as read-only.
type GraphSnapshot = analysis.GraphSnapshot

// Inspect reads the Git file catalog and organization metadata without capturing
// source bytes, computing a digest or building a graph.
func Inspect(ctx context.Context, req InputRequest) (InspectReport, error) {
	return analysis.Inspect(ctx, req)
}

// Snapshot captures content identity independently of organization configuration.
func Snapshot(ctx context.Context, req InputRequest) (SnapshotReport, error) {
	return analysis.CaptureSnapshot(ctx, req)
}

// Diff compares selected contents and estimates affected files/tests. It never
// executes tests. Partial evidence remains usable according to caller policy.
func Diff(ctx context.Context, req DiffRequest) (DiffReport, error) {
	return analysis.Analyze(ctx, req)
}

// Graph captures source bytes and builds a graph from the same version.
// maxDocuments must be positive; omitted documents are reported as diagnostics.
func Graph(ctx context.Context, req InputRequest, maxDocuments int) (*GraphSnapshot, error) {
	return analysis.CaptureGraph(ctx, req, maxDocuments)
}

// TreeReport describes entries independently of Component discovery.
type TreeReport = analysis.TreeReport
type Directory = analysis.Directory

// GitEntry retains literal path, entry kind, mode and observed object ID.
type GitEntry = git.Entry
type File = analysis.File
type Manifest = analysis.Manifest

// Tree observes paths, kinds and known file roles in the selected version.
func Tree(ctx context.Context, req InputRequest) (TreeReport, error) { return analysis.Tree(ctx, req) }
