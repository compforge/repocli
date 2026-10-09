package analysis

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	shared "github.com/compforge/codegraph"
)

// DeadcodeRequest selects repository contents and finite graph build budgets.
// Zero budgets retain CodeGraph defaults.
type DeadcodeRequest struct {
	InputRequest
	MaxNodes     int
	MaxRelations int
}

func (req DeadcodeRequest) Validate() error {
	if req.MaxNodes < 0 || req.MaxRelations < 0 {
		return fmt.Errorf("MaxNodes and MaxRelations must not be negative")
	}
	if req.Head != "" && req.Staged {
		return fmt.Errorf("head and staged are mutually exclusive")
	}
	return nil
}

// DeadcodeReport contains declaration candidates with no incoming usage relation.
// Capture completeness and graph diagnostics retain their independent meanings.
type DeadcodeReport struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Snapshot      SnapshotReport      `json:"snapshot"`
	Documents     int                 `json:"documents"`
	Nodes         []shared.Node       `json:"nodes"`
	Diagnostics   []shared.Diagnostic `json:"diagnostics"`
}

func AnalyzeDeadcode(ctx context.Context, req DeadcodeRequest) (DeadcodeReport, error) {
	if err := req.Validate(); err != nil {
		return DeadcodeReport{}, err
	}
	captured, err := Capture(ctx, req.InputRequest)
	if err != nil {
		return DeadcodeReport{}, err
	}
	// Analyze all captured text documents; viewer display/admission limits must
	// not turn an omitted caller into a deadcode candidate.
	graph, err := buildGraphSnapshot(ctx, captured, max(1, len(captured.Contents.Files)), shared.Options{MaxNodes: req.MaxNodes, MaxRelations: req.MaxRelations})
	if err != nil {
		return DeadcodeReport{}, err
	}
	nodes, err := unreferencedDeclarations(ctx, graph)
	if err != nil {
		return DeadcodeReport{}, err
	}
	return DeadcodeReport{SchemaVersion: 1, Snapshot: graph.Snapshot,
		Documents: graph.Documents, Nodes: nodes, Diagnostics: graph.Diagnostics}, nil
}

// +spec=`Deadcode candidates have no incoming non-structural relation, regardless of confidence`
// This is an observation about the captured graph, not proof of safe deletion.
func unreferencedDeclarations(ctx context.Context, graph *GraphSnapshot) ([]shared.Node, error) {
	declared, used := map[string]bool{}, map[string]bool{}
	for _, edge := range graph.Relations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		switch edge.Kind {
		case shared.Declares:
			declared[edge.Target] = true
		case shared.Contains, shared.Encloses, shared.InNamespace, shared.InDirectory, shared.OccursIn:
			// Ownership and source-location edges are not uses.
		default:
			used[edge.Target] = true
		}
	}
	nodes := []shared.Node{}
	for _, node := range graph.Nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !declared[node.ID] || used[node.ID] {
			continue
		}
		switch node.Kind {
		case shared.DocumentNodeKind, shared.DirectoryNodeKind, shared.Module, shared.Package, shared.Namespace:
			continue
		}
		nodes = append(nodes, node)
	}
	slices.SortFunc(nodes, func(a, b shared.Node) int {
		var left, right shared.Location
		if a.Location != nil {
			left = *a.Location
		}
		if b.Location != nil {
			right = *b.Location
		}
		if c := cmp.Compare(left.Path, right.Path); c != 0 {
			return c
		}
		if c := cmp.Compare(left.StartByte, right.StartByte); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
	return nodes, nil
}
