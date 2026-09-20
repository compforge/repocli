package codegraph

import (
	"context"
	"slices"
	"sync"

	shared "github.com/compforge/codegraph"
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

var supportedLanguages sync.Map

// +rule=`A registered grammar without declaration capability is not a repocli source language`
func Language(name string) string {
	language := shared.Language(name)
	if language == "" {
		return ""
	}
	if supported, ok := supportedLanguages.Load(language); ok {
		if supported.(bool) {
			return language
		}
		return ""
	}
	capabilities := shared.Capabilities(language)
	supported := len(capabilities) == 1 && len(capabilities[0].Declarations) > 0
	supportedLanguages.Store(language, supported)
	if supported {
		return language
	}
	return ""
}

// AnalyzeFeatures uses CodeGraph for declarations and same-file calls while
// retaining repocli's syntax facts for imports, config context, and exports.
// Those latter facts are consumer policy; the source graph itself owns the
// language-neutral declaration and relation extraction.
// +why=`Repository-aware resolution and test selection must not leak into the shared source graph`
func (a *Analyzer) AnalyzeFeatures(ctx context.Context, name string, data []byte, features syntax.Features) syntax.Facts {
	facts := a.syntax.AnalyzeFeatures(ctx, name, data, features)
	if !features.Symbols && !features.Calls {
		return facts
	}
	facts.Issues = slices.DeleteFunc(facts.Issues, func(issue syntax.Issue) bool {
		return features.Symbols && issue.Feature == syntax.Symbols
	})
	graphFacts := sharedSource(ctx, name, data)
	facts.Language = graphFacts.Language
	if features.Symbols {
		facts.Symbols = make([]syntax.Symbol, 0, len(graphFacts.Symbols))
		for _, symbol := range graphFacts.Symbols {
			facts.Symbols = append(facts.Symbols, syntax.Symbol{
				QualifiedName: symbol.QualifiedName,
				Parent:        graphFacts.parents[symbol.QualifiedName],
				Name:          symbol.Name,
				Kind:          symbol.Kind,
				StartLine:     symbol.StartLine,
				EndLine:       symbol.EndLine,
			})
		}
	}
	if features.Calls {
		facts.Calls = append([]syntax.LocalCall(nil), graphFacts.calls...)
	}
	for _, issue := range graphFacts.Issues {
		facts.Issues = appendSharedIssue(facts.Issues, syntax.Issue{
			Code: issue.Code, Message: issue.Message, Line: issue.Line,
		})
	}
	return facts
}

func (a *Analyzer) Source(ctx context.Context, name string, data []byte) Source {
	return sharedSource(ctx, name, data).Source
}

func appendSharedIssue(issues []syntax.Issue, issue syntax.Issue) []syntax.Issue {
	for _, existing := range issues {
		if existing.Code == issue.Code && (existing.Line == issue.Line || existing.Line == 0 || issue.Line == 0) {
			return issues
		}
	}
	return append(issues, issue)
}
