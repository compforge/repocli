package impact

import (
	"reflect"
	"testing"
)

func TestGoDeclarationChangeKeepsPackageImpact(t *testing.T) {
	for _, tc := range []struct{ source, old, new, symbol string }{
		{"package core\ntype Value int\n", "int", "string", "Value"},
		{"package core\ntype Value = int\n", "int", "string", "Value"},
		{"package core\nconst Value = 1\n", "1", "2", "Value"},
		{"package core\nvar Value = 1\n", "1", "2", "Value"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			r := runChange(t, map[string]string{
				"go.mod":               "module example.com/sample\n\ngo 1.25.0\n",
				"core/value.go":        tc.source,
				"tests/core_test.go":   "package tests\nimport _ \"example.com/sample/core\"\n",
				"tests/helper_test.go": "package tests\nfunc helper() {}\n",
			}, "core/value.go", tc.old, tc.new)
			if r.Scope != "focused" || !reflect.DeepEqual(r.TestFiles, []string{"tests/core_test.go", "tests/helper_test.go"}) {
				t.Fatalf("result: %+v", r)
			}
			change := r.Changes[0]
			if len(change.Before) != 1 || len(change.After) != 1 || change.Before[0].Name != tc.symbol || change.After[0].Name != tc.symbol {
				t.Fatalf("declarations missing: %+v", change)
			}
		})
	}
}

func TestUncoveredAssignmentUsesFileSeed(t *testing.T) {
	r := runChange(t, map[string]string{
		"src/values.py":       "VALUE = 1\nOTHER = 2\n",
		"tests/test_value.py": "from ..src.values import VALUE\n",
		"tests/test_other.py": "from ..src.values import OTHER\n",
	}, "src/values.py", "VALUE = 1", "VALUE = 3")
	if !reflect.DeepEqual(r.TestFiles, []string{"tests/test_other.py", "tests/test_value.py"}) {
		t.Fatalf("uncovered assignment lost file impact: %+v", r)
	}
	if len(r.Changes[0].After) != 0 {
		t.Fatalf("invented assignment declaration: %+v", r.Changes[0])
	}
}
