package analysis

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"unicode/utf8"

	shared "github.com/compforge/codegraph"
	"github.com/compforge/repocli/toolkit/go/internal/git"
	"github.com/compforge/repocli/toolkit/go/internal/project"
)

// GraphSnapshot retains the source bytes behind a published graph. The viewer
// consumes shared graph facts directly, without the impact-specific projection.
// +spec=`Graph nodes, relations and displayed source belong to one captured snapshot`
type GraphSnapshot struct {
	project.Layout
	Snapshot    SnapshotReport      `json:"snapshot"`
	Nodes       []shared.Node       `json:"nodes"`
	Relations   []shared.Relation   `json:"relations"`
	Diagnostics []shared.Diagnostic `json:"diagnostics"`
	Documents   int                 `json:"documents"`
	Sources     map[string][]byte   `json:"-"`
}

func CaptureGraph(ctx context.Context, req InputRequest, maxDocuments int) (*GraphSnapshot, error) {
	if maxDocuments <= 0 {
		return nil, fmt.Errorf("document limit must be positive")
	}
	captured, err := Capture(ctx, req)
	if err != nil {
		return nil, err
	}
	// Full repository graphs include source-use nodes and relations; the
	// general-purpose CodeGraph defaults can exhaust these before binding ends.
	return buildGraphSnapshot(ctx, captured, maxDocuments, shared.Options{
		MaxNodes: 250000, MaxRelations: 1000000,
	})
}

// +spec=Shared tooling tags exclude graph material before document budgets while the captured snapshot remains intact.
// +spec=Only source material is parsed for symbols; other documents remain context and recognized manifests supply metadata.
// +link=repo://docs/view.md
func buildGraphSnapshot(ctx context.Context, captured *CapturedSnapshot, maxDocuments int, opts shared.Options) (*GraphSnapshot, error) {
	report, contents := captured.Report, captured.Contents
	repo := &git.Repository{Root: report.Checkout}
	origin, err := repo.Origin(ctx)
	if err != nil {
		return nil, err
	}
	layout, err := project.Load(contents.Files, origin)
	if err != nil {
		return nil, err
	}
	ignored, err := git.IgnoredFiles(ctx, contents.Files)
	if err != nil {
		return nil, fmt.Errorf("select graph documents: %w", err)
	}
	if opts.Admission == nil {
		opts.Admission = &shared.AdmissionPolicy{
			ParseTags:    []shared.Tag{shared.SourceTag},
			ExcludedTags: []shared.Tag{shared.ToolingTag},
		}
	}
	matcher, err := shared.NewTagMatcher(opts.TagRules)
	if err != nil {
		return nil, fmt.Errorf("classify graph documents: %w", err)
	}
	names := make([]string, 0, len(contents.Files))
	for name := range contents.Files {
		if ignored[name] || matcher.ExcludedBy(shared.Document{Path: name}, opts.Admission.ExcludedTags) != "" {
			continue
		}
		names = append(names, name)
	}
	slices.Sort(names)
	docs := make([]shared.Document, 0, min(len(names), maxDocuments))
	sources := make(map[string][]byte)
	var omitted int
	for _, name := range names {
		data := contents.Files[name]
		if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
			continue
		}
		if len(docs) >= maxDocuments {
			omitted++
			continue
		}
		docs = append(docs, shared.Document{Path: name, Content: data})
		sources[name] = data
	}
	opts.MaxDocuments = maxDocuments
	opts.MaxSourceBytes = 128 << 20
	graph, build, err := shared.Build(ctx, report.Snapshot, docs, opts)
	if err != nil {
		return nil, fmt.Errorf("build code graph: %w", err)
	}
	diagnostics := build.Diagnostics
	if omitted > 0 {
		diagnostics = append(diagnostics, shared.Diagnostic{Code: "document_limit", Subject: shared.DocumentSubject, Message: fmt.Sprintf("%d text documents omitted by --max-documents", omitted)})
	}
	if diagnostics == nil {
		diagnostics = []shared.Diagnostic{}
	}
	return &GraphSnapshot{Layout: layout, Snapshot: report, Nodes: graph.Nodes(), Relations: graph.Relations(),
		Diagnostics: diagnostics, Documents: len(docs), Sources: sources}, nil
}
