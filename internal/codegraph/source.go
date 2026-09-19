package codegraph

import (
	"context"

	"github.com/compforge/repocli/internal/codegraph/internal/syntax"
)

// Analyzer owns syntax adaptation and reuses immutable source facts within a run.
// Consumers see declarations and graph evidence, never parser-specific objects.
type Analyzer struct{ syntax syntax.Analyzer }

type Symbol struct {
	QualifiedName string `json:"qualifiedName"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	StartLine     int    `json:"startLine"`
	EndLine       int    `json:"endLine"`
}

type Source struct {
	Language string
	Symbols  []Symbol
	Issues   []Issue
}

func Language(name string) string { return syntax.Language(name) }

func (a *Analyzer) Source(ctx context.Context, name string, data []byte) Source {
	facts := a.syntax.AnalyzeFeatures(ctx, name, data, syntax.Features{Symbols: true})
	source := Source{Language: facts.Language, Symbols: []Symbol{}}
	for _, symbol := range facts.Symbols {
		source.Symbols = append(source.Symbols, Symbol{QualifiedName: symbol.QualifiedName, Name: symbol.Name, Kind: symbol.Kind, StartLine: symbol.StartLine, EndLine: symbol.EndLine})
	}
	for _, issue := range facts.Issues {
		source.Issues = append(source.Issues, Issue{Path: name, Code: issue.Code, Message: issue.Message, Line: issue.Line, Kind: kindForFeature(issue.Feature)})
	}
	return source
}
