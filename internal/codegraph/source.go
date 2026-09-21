package codegraph

import (
	"sync"

	shared "github.com/compforge/codegraph"
)

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
