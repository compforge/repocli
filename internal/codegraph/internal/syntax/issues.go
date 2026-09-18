package syntax

// Feature identifies the facts a consumer needs, independently of its purpose.
type Feature string

const (
	Imports Feature = "imports"
	Symbols Feature = "symbols"
	Calls   Feature = "calls"
)

type Features struct{ Symbols, Calls bool }

// Issue preserves extraction provenance; an empty Feature affects any query.
type Issue struct {
	Code, Message string
	Feature       Feature
	Line          int
	Symbol        string
}

func (i Issue) String() string { return i.Message }
func (f *Facts) issue(code string, feature Feature, line int, message string) {
	f.Issues = append(f.Issues, Issue{Code: code, Feature: feature, Line: line, Message: message})
}
