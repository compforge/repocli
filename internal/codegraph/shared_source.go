package codegraph

import (
	"sort"
	"strings"

	shared "github.com/compforge/codegraph"
)

// sharedSourceResult is the consumer-side boundary between the generic CodeGraph
// model and repocli's historical wire shape. CodeGraph owns parsing,
// declaration categories, source locations, and static relations; repocli
// retains its test-selection and configuration-resolution policies.
type sharedSourceResult struct {
	Source
	calls   []localCall
	parents map[string]string
}

type localCall struct {
	caller, callee string
	calleeFile     string
	id             string
	location       shared.Location
	confidence     Confidence
	basis          string
	line           int
}

// projectSources visits the shared graph once, keeping source identities intact.
// Repository imports remain consumer-owned; calls preserve both endpoint files.
func projectSources(g *shared.Graph, report shared.BuildReport) map[string]sharedSourceResult {
	results := map[string]sharedSourceResult{}
	byID := map[string]shared.Node{}
	nodes := g.Nodes()
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Location.StartByte != nodes[j].Location.StartByte {
			return nodes[i].Location.StartByte < nodes[j].Location.StartByte
		}
		return nodes[i].ID < nodes[j].ID
	})
	for _, name := range report.Files {
		results[name] = sharedSourceResult{Source: Source{Language: Language(name), Symbols: []Symbol{}}, parents: map[string]string{}}
	}
	for _, node := range nodes {
		byID[node.ID] = node
		if node.Kind == shared.File {
			continue
		}
		name := node.Location.Path
		result := results[name]
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
		results[name] = result
	}
	for _, relation := range g.Relations() {
		from, fromOK := byID[relation.Source]
		to, toOK := byID[relation.Target]
		if !fromOK || !toOK {
			continue
		}
		result := results[from.Location.Path]
		switch relation.Kind {
		case shared.Contains:
			if from.Kind != shared.File && from.Location.Path == to.Location.Path && relation.Confidence == shared.Exact {
				result.parents[to.QualifiedName] = from.QualifiedName
			}
		case shared.Calls:
			call := localCall{caller: from.QualifiedName, callee: to.QualifiedName, calleeFile: to.Location.Path,
				line: relation.Location.Line, confidence: Confidence(relation.Confidence), basis: relation.Basis,
				id: relation.ID, location: relation.Location}
			result.calls = append(result.calls, call)
		}
		results[from.Location.Path] = result
	}
	for _, diagnostic := range report.Diagnostics {
		name := diagnostic.Location.Path
		result := results[name]
		// Failed parses have diagnostics but no declaration nodes or report file.
		result.Language = Language(name)
		if keepBuildDiagnostic(diagnostic.Code) {
			result.Diagnostics = append(result.Diagnostics, projectDiagnostic(diagnostic))
			results[name] = result
		}
	}
	return results
}

func projectDiagnostic(d shared.Diagnostic) Diagnostic {
	return Diagnostic{Path: d.Location.Path, Code: d.Code, Message: d.Message,
		Line: d.Location.Line, Kind: Kind(d.Relation), Subject: d.Subject, Location: d.Location, Outline: d.Outline}
}

func keepBuildDiagnostic(code string) bool {
	switch code {
	case "parse_error", "outline_incomplete", "unsupported_declaration", "unresolved_owner", "unsupported_resolution", "unsupported_language", "shared_codegraph_error", "context_limit", "unresolved_call":
		return true
	default:
		// Relation diagnostics belong to repocli's repository-aware resolver
		// and consumer policy, not the generic source projection.
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
