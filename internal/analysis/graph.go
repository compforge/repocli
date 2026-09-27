package analysis

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"unicode/utf8"

	shared "github.com/compforge/codegraph"
	"golang.org/x/mod/modfile"
)

// GraphSnapshot retains the source bytes behind a published graph. The viewer
// consumes shared graph facts directly, without the impact-specific projection.
// +spec=`Graph nodes, relations and displayed source belong to one captured snapshot`
type GraphSnapshot struct {
	Snapshot    SnapshotReport      `json:"snapshot"`
	Nodes       []shared.Node       `json:"nodes"`
	Relations   []shared.Relation   `json:"relations"`
	Diagnostics []shared.Diagnostic `json:"diagnostics"`
	Documents   int                 `json:"documents"`
	Sources     map[string][]byte   `json:"-"`
}

func CaptureGraph(ctx context.Context, req SnapshotRequest, maxDocuments int) (*GraphSnapshot, error) {
	if maxDocuments <= 0 {
		return nil, fmt.Errorf("document limit must be positive")
	}
	report, contents, err := captureContents(ctx, req)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(contents.Files))
	for name := range contents.Files {
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
	opts := shared.Options{MaxDocuments: maxDocuments, MaxSourceBytes: 128 << 20}
	if data, ok := contents.Files["go.mod"]; ok {
		opts.ModulePath = modfile.ModulePath(data)
	}
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
	return &GraphSnapshot{Snapshot: report, Nodes: graph.Nodes(), Relations: graph.Relations(),
		Diagnostics: diagnostics, Documents: len(docs), Sources: sources}, nil
}
