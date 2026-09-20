package codegraph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing/fstest"

	shared "github.com/compforge/codegraph"
	"github.com/compforge/repocli/internal/codegraph/internal/syntax"
)

// sharedSource is the consumer-side boundary between the generic CodeGraph
// model and repocli's historical wire shape. CodeGraph owns parsing,
// declaration categories, source locations, and static relations; repocli
// retains its test-selection and configuration-resolution policies.
type sharedSourceResult struct {
	Source
	calls   []syntax.LocalCall
	parents map[string]string
}

func sharedSource(ctx context.Context, name string, data []byte) sharedSourceResult {
	language := Language(name)
	if language == "" {
		return sharedSourceResult{Source: Source{Symbols: []Symbol{}}, parents: map[string]string{}}
	}
	fsys := fstest.MapFS{name: {Data: append([]byte(nil), data...)}}
	snapshot := sha256.Sum256(data)
	g, report, err := shared.Build(ctx, "repocli:"+hex.EncodeToString(snapshot[:]), fsys, []string{name}, shared.Options{})
	if err != nil {
		return sharedSourceResult{Source: Source{Language: language, Issues: []Issue{{Path: name, Code: "shared_codegraph_error", Message: err.Error()}}}}
	}
	result := sharedSourceResult{Source: Source{Language: language, Symbols: []Symbol{}}, parents: map[string]string{}}
	byID := map[string]shared.Node{}
	for _, node := range g.Nodes() {
		byID[node.ID] = node
	}
	for _, node := range g.Find(name, "", "") {
		result.Symbols = append(result.Symbols, Symbol{
			QualifiedName: node.QualifiedName,
			Name:          node.Name,
			Kind:          sharedKind(node.Kind),
			StartLine:     node.Location.Line,
			EndLine:       node.Location.EndLine,
		})
		if strings.HasSuffix(node.QualifiedName, "."+node.Name) {
			result.parents[node.QualifiedName] = strings.TrimSuffix(node.QualifiedName, "."+node.Name)
		}
	}
	for _, relation := range g.Relations() {
		from, fromOK := byID[relation.Source]
		to, toOK := byID[relation.Target]
		if !fromOK || !toOK {
			continue
		}
		switch relation.Kind {
		case shared.Contains:
			if from.Kind != shared.File && relation.Confidence == shared.Exact {
				result.parents[to.QualifiedName] = from.QualifiedName
			}
		case shared.Calls:
			if relation.Confidence == shared.Exact {
				result.calls = append(result.calls, syntax.LocalCall{Caller: from.QualifiedName, Callee: to.QualifiedName, Line: relation.Location.Line})
			}
		}
	}
	for _, diagnostic := range report.Diagnostics {
		if keepSharedIssue(diagnostic.Code) {
			result.Issues = append(result.Issues, Issue{Path: name, Code: diagnostic.Code, Message: diagnostic.Message, Line: diagnostic.Location.Line})
		}
	}
	return result
}

func keepSharedIssue(code string) bool {
	switch code {
	case "parse_error", "outline_incomplete", "unsupported_declaration", "unresolved_owner", "unsupported_resolution", "unsupported_language", "shared_codegraph_error":
		return true
	default:
		// Relation diagnostics belong to repocli's repository-aware resolver
		// and consumer policy, not this single-file adapter.
		return false
	}
}

func sharedKind(kind shared.NodeKind) string {
	switch kind {
	case shared.Class:
		return "class"
	case shared.Struct:
		return "struct"
	case shared.Interface:
		return "interface"
	case shared.Field:
		return "field"
	case shared.Method:
		return "method"
	case shared.Function:
		return "function"
	case shared.TypeAlias:
		return "type_alias"
	case shared.Type:
		return "type"
	case shared.Variable:
		return "variable"
	case shared.Constant:
		return "constant"
	case shared.Enum:
		return "enum"
	case shared.Record:
		return "record"
	case shared.Constructor:
		return "constructor"
	case shared.Module:
		return "module"
	case shared.Namespace:
		return "namespace"
	case shared.Property:
		return "property"
	case shared.Trait:
		return "trait"
	case shared.Macro:
		return "macro"
	case shared.Union:
		return "union"
	default:
		return strings.ToLower(string(kind))
	}
}
