package units

import (
	"fmt"
	"sort"
)

// Kind describes syntax in a changed source region. It is owned by repocli,
// independently of the node vocabulary of any relationship provider.
type Kind string

const (
	Unknown     Kind = "unknown"
	Function    Kind = "function"
	Method      Kind = "method"
	Class       Kind = "class"
	Struct      Kind = "struct"
	Interface   Kind = "interface"
	Import      Kind = "import"
	Export      Kind = "export"
	Type        Kind = "type"
	Enum        Kind = "enum"
	Field       Kind = "field"
	Property    Kind = "property"
	Variable    Kind = "variable"
	Constant    Kind = "constant"
	Module      Kind = "module"
	Namespace   Kind = "namespace"
	Constructor Kind = "constructor"
	Record      Kind = "record"
	Whitespace  Kind = "whitespace"
)

// Element describes a source declaration or directive, without graph identity.
// Span includes associated documentation where the parser provides ownership.
type Element struct {
	Path      string `json:"path"`
	Name      string `json:"name,omitempty"`
	Kind      Kind   `json:"kind"`
	Span      Span   `json:"span"`
	StartByte int    `json:"start_byte"`
	EndByte   int    `json:"end_byte"`
}

func elementID(e Element) string {
	return fmt.Sprintf("%s:%d:%d:%s:%s", e.Path, e.StartByte, e.EndByte, e.Kind, e.Name)
}

func sortElementsBySize(es []Element) {
	sort.Slice(es, func(i, j int) bool {
		a, b := es[i], es[j]
		// A line patch cannot isolate a nested element sharing the entire line.
		if a.Span == b.Span && a.EndByte-a.StartByte != b.EndByte-b.StartByte {
			return a.EndByte-a.StartByte > b.EndByte-b.StartByte
		}
		if a.EndByte-a.StartByte != b.EndByte-b.StartByte {
			return a.EndByte-a.StartByte < b.EndByte-b.StartByte
		}
		return elementID(a) < elementID(b)
	})
}

// ElementAt chooses the innermost source owner; relationship evidence does not
// affect how the patch is partitioned.
func ElementAt(es []Element, line int) (Element, bool) {
	for _, e := range es {
		if e.Span.Start <= line && line <= e.Span.End {
			return e, true
		}
	}
	return Element{}, false
}
