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
	line           int
}

// projectSources visits the shared graph once, keeping source identities intact.
// Repository imports remain consumer-owned; calls retain the same-file contract.
func projectSources(g *shared.Graph, report shared.BuildReport) map[string]sharedSourceResult {
	results := map[string]sharedSourceResult{}
	byID := map[string]shared.Node{}
	functions := map[string]map[string]bool{}
	nodes := g.Nodes()
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Location.StartByte != nodes[j].Location.StartByte {
			return nodes[i].Location.StartByte < nodes[j].Location.StartByte
		}
		return nodes[i].ID < nodes[j].ID
	})
	for _, name := range report.Files {
		results[name] = sharedSourceResult{Source: Source{Language: Language(name), Symbols: []Symbol{}}, parents: map[string]string{}}
		functions[name] = map[string]bool{}
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
		if node.Kind == shared.Function && node.QualifiedName == node.Name {
			functions[name][node.Name] = true
		}
		if strings.HasSuffix(node.QualifiedName, "."+node.Name) {
			result.parents[node.QualifiedName] = strings.TrimSuffix(node.QualifiedName, "."+node.Name)
		}
		results[name] = result
	}
	for _, relation := range g.Relations() {
		from, fromOK := byID[relation.Source]
		to, toOK := byID[relation.Target]
		if !fromOK || !toOK || from.Location.Path != to.Location.Path {
			continue
		}
		result := results[from.Location.Path]
		switch relation.Kind {
		case shared.Contains:
			if from.Kind != shared.File && relation.Confidence == shared.Exact {
				result.parents[to.QualifiedName] = from.QualifiedName
			}
		case shared.Calls:
			if relation.Confidence == shared.Exact {
				result.calls = append(result.calls, localCall{caller: from.QualifiedName, callee: to.QualifiedName, line: relation.Location.Line})
			}
		}
		results[from.Location.Path] = result
	}
	for _, diagnostic := range report.Diagnostics {
		name := diagnostic.Location.Path
		result := results[name]
		// Failed parses have diagnostics but no declaration nodes or report file.
		result.Language = Language(name)
		if keepSharedIssue(diagnostic.Code, diagnostic.Message, functions[name]) {
			result.Issues = append(result.Issues, Issue{Path: name, Kind: sharedIssueKind(diagnostic.Code), Code: diagnostic.Code, Message: diagnostic.Message, Line: diagnostic.Location.Line})
			results[name] = result
		}
	}
	return results
}

func keepSharedIssue(code, reference string, functions map[string]bool) bool {
	switch code {
	case "parse_error", "outline_incomplete", "unsupported_declaration", "unresolved_owner", "unsupported_resolution", "unsupported_language", "shared_codegraph_error":
		return true
	case "dynamic_call", "unresolved_call", "ambiguous_call":
		// Only retain a local-call gap when a declared module function could
		// have been selected. Other calls (for example Path().insert()) are
		// unrelated to the source relation query and remain resolver context.
		return functions[reference]
	default:
		// Relation diagnostics belong to repocli's repository-aware resolver
		// and consumer policy, not the generic source projection.
		return false
	}
}

func sharedIssueKind(code string) Kind {
	switch code {
	case "dynamic_call", "unresolved_call", "ambiguous_call":
		return Calls
	default:
		return ""
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
