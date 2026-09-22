package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/compforge/repocli/internal/analysis"
)

func TestTextReportExplainsDependencyGap(t *testing.T) {
	report := analysis.Report{Diagnostics: []analysis.Diagnostic{{
		Code: "impact_uncertain", Path: "src/loader.ts", Line: 42,
		Reason: "unresolved_import", Relation: "imports", Version: "after",
		Message: "Cannot resolve ./generated; an unselected candidate may depend on it",
	}}}
	var out bytes.Buffer
	if err := writeReport(&out, report, false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"src/loader.ts:42", "reason=unresolved_import", "relation=imports", "version=after", "./generated", "additional affected tests"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in %s", want, out.String())
		}
	}
}
