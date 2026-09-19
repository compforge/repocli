package impact

import (
	"context"
	"github.com/compforge/repocli/internal/codegraph"
	"github.com/compforge/repocli/internal/diff"
	"reflect"
	"testing"
)

func TestHelperChangeReachesImportedCaller(t *testing.T) {
	before := files(map[string]string{
		"lib.py":              "def helper():\n    return 1\ndef api():\n    return helper()\ndef unrelated():\n    return 3\n",
		"tests/test_api.py":   "from ..lib import api\n",
		"tests/test_other.py": "from ..lib import unrelated\n",
	})
	after := files(map[string]string{
		"lib.py":              "def helper():\n    return 2\ndef api():\n    return helper()\ndef unrelated():\n    return 3\n",
		"tests/test_api.py":   "from ..lib import api\n",
		"tests/test_other.py": "from ..lib import unrelated\n",
	})
	got, err := Analyze(context.Background(), Request{Before: before, After: after, TestDirs: []string{"tests"}, Changes: []diff.Change{
		{Path: "lib.py", Status: "modified", Hunks: []diff.Hunk{{Old: diff.Range{Start: 2, Count: 1}, New: diff.Range{Start: 2, Count: 1}}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.TestFiles, []string{"tests/test_api.py"}) || got.Scope != "focused" {
		t.Fatalf("%+v", got)
	}
	if got.Reasons[0].Relations[1].Kind != codegraph.Calls {
		t.Fatal(got.Reasons)
	}
}

func TestComparisonDoesNotInventCrossVersionPath(t *testing.T) {
	before, after := codegraph.New(), codegraph.New()
	before.AddRelation(codegraph.Relation{From: "bridge", To: "seed", Kind: codegraph.Imports})
	after.AddRelation(codegraph.Relation{From: "candidate", To: "bridge", Kind: codegraph.Imports})
	paths := impactPaths([]*codegraph.Graph{before, after}, []string{"seed"}, false)
	if _, ok := paths["candidate"]; ok {
		t.Fatal("joined edges from incompatible versions")
	}
	if _, ok := paths["bridge"]; !ok {
		t.Fatal("lost old relation")
	}
}

func TestPythonPathContextSelectsOnlyProvenConsumer(t *testing.T) {
	before := files(map[string]string{
		"one/dep.py":            "def value():\n    return 1\n",
		"two/dep.py":            "def value():\n    return 2\n",
		"tests/test_one.py":     "from pathlib import Path\nimport sys\nroot=Path(__file__).parents[1] / 'one'\nsys.path.insert(0,str(root))\nfrom dep import value\n",
		"tests/test_two.py":     "from pathlib import Path\nimport sys\nroot=Path(__file__).parents[1] / 'two'\nsys.path.insert(0,str(root))\nfrom dep import value\n",
		"tests/test_unknown.py": "from dep import value\n",
	})
	after := map[string][]byte{}
	for name, data := range before {
		after[name] = data
	}
	after["one/dep.py"] = []byte("def value():\n    return 3\n")
	got, err := Analyze(context.Background(), Request{Before: before, After: after, TestDirs: []string{"tests"}, Changes: []diff.Change{
		{Path: "one/dep.py", Status: "modified", Hunks: []diff.Hunk{{Old: diff.Range{Start: 2, Count: 1}, New: diff.Range{Start: 2, Count: 1}}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.TestFiles, []string{"tests/test_one.py"}) || got.Scope != "partial" {
		t.Fatalf("%+v", got)
	}
	found := false
	for _, issue := range got.Uncertainties {
		if issue.Path == "tests/test_unknown.py" && issue.Confidence == codegraph.Weak {
			found = true
		}
	}
	if !found {
		t.Fatalf("lost inferred consumer: %+v", got.Uncertainties)
	}
}
