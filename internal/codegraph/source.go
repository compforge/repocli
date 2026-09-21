package codegraph

import (
	"context"
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

// Analyze retains only the repository-resolution facts owned by repocli.
// Declarations and calls come directly from the shared CodeGraph source model.
// +why=`Repository-aware resolution and test selection must not leak into the shared source graph`
func (a *Analyzer) Analyze(ctx context.Context, name string, data []byte) syntax.Facts {
	return a.syntax.Analyze(ctx, name, data)
}

func (a *Analyzer) Source(ctx context.Context, name string, data []byte) Source {
	return sharedSource(ctx, name, data).Source
}
