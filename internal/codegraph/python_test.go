package codegraph

import (
	"context"
	"slices"
	"strings"
	"testing"
)

const pythonPrelude = "from pathlib import Path\nimport sys\nROOT = Path(__file__).parent\n"

func pythonGraph(t *testing.T, source string, extra map[string]string) BuildResult {
	t.Helper()
	files := map[string][]byte{"entry.py": []byte(source), "one/dep.py": []byte("VALUE=1\n"), "two/dep.py": []byte("VALUE=2\n")}
	for name, source := range extra {
		files[name] = []byte(source)
	}
	built, err := Build(context.Background(), BuildRequest{BuildOptions: BuildOptions{Files: files, Kinds: []Kind{Imports}, MaxFiles: 100, MaxDepth: 10}, FilesToExpand: []string{"entry.py"}})
	if err != nil {
		t.Fatal(err)
	}
	return built
}

func pythonFileEdges(built BuildResult) []Relation {
	var out []Relation
	for _, edge := range built.Graph.Outgoing("entry.py") {
		if built.Graph.Nodes[edge.To].Kind == "file" && strings.HasSuffix(edge.To, ".py") {
			out = append(out, edge)
		}
	}
	return out
}

func TestPythonContextUsesStatementOrder(t *testing.T) {
	built := pythonGraph(t, pythonPrelude+`import dep
vendor = ROOT / "one"
sys.path.insert(0, str(vendor))
import dep
sys.path.insert(0, str(ROOT / "two"))
import dep
`, nil)
	edges := pythonFileEdges(built)
	counts := map[int]int{}
	for _, edge := range edges {
		counts[edge.Line]++
		switch edge.Line {
		case 4:
			if edge.Confidence != Weak {
				t.Fatalf("later path changed earlier import: %+v", edge)
			}
		case 7:
			if edge.To != "one/dep.py" || edge.Confidence != "" {
				t.Fatalf("variable path unresolved: %+v", edge)
			}
		case 9:
			if edge.To != "two/dep.py" || edge.Confidence != "" {
				t.Fatalf("insert order lost: %+v", edge)
			}
		default:
			t.Fatalf("unexpected import: %+v", edge)
		}
	}
	if counts[4] != 2 || counts[7] != 1 || counts[9] != 1 || len(built.Issues) != 0 {
		t.Fatalf("edges=%+v issues=%+v", edges, built.Issues)
	}
}

func TestPythonBranchesAndDeferredBodiesDoNotInventContext(t *testing.T) {
	for _, body := range []string{
		"if flag:\n    sys.path.insert(0, str(ROOT / 'one'))\n",
		"def setup():\n    sys.path.insert(0, str(ROOT / 'one'))\n",
		"if flag:\n    sys.path.insert(0, str(ROOT / 'one'))\nelse:\n    sys.path.insert(0, str(ROOT / 'two'))\n",
	} {
		built := pythonGraph(t, pythonPrelude+body+"import dep\n", nil)
		for _, edge := range pythonFileEdges(built) {
			if edge.Confidence == "" {
				t.Fatalf("invented definite relation for %q: %+v", body, edge)
			}
		}
		if _, ok := built.Graph.Reverse([]string{"one/dep.py"}, []Kind{Imports})["entry.py"]; ok {
			t.Fatal("inference entered definite traversal")
		}
	}
	built := pythonGraph(t, pythonPrelude+`if flag:
    vendor=ROOT / "one"
else:
    vendor=ROOT / "one"
sys.path.insert(0,str(vendor))
import dep
`, nil)
	edges := pythonFileEdges(built)
	if len(edges) != 1 || edges[0].To != "one/dep.py" || edges[0].Confidence != "" {
		t.Fatalf("equal branch values were not preserved: %+v", edges)
	}
}

func TestPythonBoundedLoopsAndPathPrecedence(t *testing.T) {
	built := pythonGraph(t, pythonPrelude+`roots=(ROOT / "one", ROOT / "two")
for directory in roots:
    sys.path.insert(0,str(directory))
import dep
`, nil)
	edges := pythonFileEdges(built)
	if len(edges) != 1 || edges[0].To != "two/dep.py" || edges[0].Confidence != "" {
		t.Fatalf("loop order: %+v issues=%+v", edges, built.Issues)
	}
	appended := pythonGraph(t, pythonPrelude+"sys.path.append(str(ROOT / 'one'))\nimport dep\n", nil)
	for _, edge := range pythonFileEdges(appended) {
		if edge.Confidence == "" {
			t.Fatal("append assumed unknown earlier roots absent")
		}
	}
}

func TestPythonBindingAliasesAndShadowing(t *testing.T) {
	alias := pythonGraph(t, "from pathlib import Path as P\nimport sys as system\np=P(__file__).parent / 'one'\nsystem.path.insert(0,str(p))\nimport dep\n", nil)
	if edges := pythonFileEdges(alias); len(edges) != 1 || edges[0].Confidence != "" {
		t.Fatalf("alias: %+v issues=%+v", edges, alias.Issues)
	}
	for _, body := range []string{
		"Path = custom\np=Path(__file__).parent / 'one'\nsys.path.insert(0,str(p))\nimport dep\n",
		"def work(Path):\n    sys.path.insert(0,str(Path(__file__).parent / 'one'))\n    import dep\n",
		"def work():\n    sys.path.insert(0,str(Path(__file__).parent / 'one'))\n    import dep\n    Path=custom\n",
		"vendor=ROOT / 'one'\nvendor=runtime_value\nsys.path.insert(0,str(vendor))\nimport dep\n",
	} {
		built := pythonGraph(t, pythonPrelude+body, nil)
		for _, edge := range pythonFileEdges(built) {
			if edge.Confidence == "" {
				t.Fatalf("shadowed binding: %q %+v", body, edge)
			}
		}
	}
}

func TestPythonWeakAndStrongTargets(t *testing.T) {
	weak := pythonGraph(t, "from only import value\n", map[string]string{"single/only.py": "value=1\n"})
	edges := pythonFileEdges(weak)
	if len(edges) != 1 || edges[0].Confidence != Weak {
		t.Fatalf("singleton catalog match upgraded: %+v", edges)
	}
	strong := pythonGraph(t, pythonPrelude+"if flag:\n    sys.path.insert(0,str(ROOT / 'one'))\nimport only\n", map[string]string{"one/only.py": "value=1\n"})
	edges = pythonFileEdges(strong)
	if len(edges) != 1 || edges[0].Confidence != Strong {
		t.Fatalf("conditional path evidence: %+v", edges)
	}
}

func TestPythonUnsupportedPathMutationsInvalidatePrefix(t *testing.T) {
	for _, mutation := range []string{
		"sys.path[:] = runtime_paths\n",
		"sys.path.clear()\n",
		"flag and sys.path.insert(0,str(ROOT / 'two'))\n",
		"for item in (ROOT / 'two', ROOT / 'one'):\n    sys.path.insert(0,str(item))\n    break\n",
	} {
		built := pythonGraph(t, pythonPrelude+"sys.path.insert(0,str(ROOT / 'one'))\n"+mutation+"import dep\n", nil)
		for _, edge := range pythonFileEdges(built) {
			if edge.Confidence == "" {
				t.Fatalf("stale prefix after %q: %+v", mutation, edge)
			}
		}
	}
}

func TestPythonCacheDoesNotReuseAnotherFilesContext(t *testing.T) {
	files := map[string][]byte{
		"a/entry.py": []byte("from pathlib import Path\nimport sys\nsys.path.insert(0,str(Path(__file__).parent))\nimport dep\n"),
		"b/entry.py": []byte("from pathlib import Path\nimport sys\nsys.path.insert(0,str(Path(__file__).parent))\nimport dep\n"),
		"a/dep.py":   []byte("value=1\n"), "b/dep.py": []byte("value=2\n"),
	}
	for _, name := range []string{"a/entry.py", "b/entry.py"} {
		built, err := Build(context.Background(), BuildRequest{BuildOptions: BuildOptions{Files: files, Kinds: []Kind{Imports}, MaxFiles: 10, MaxDepth: 3}, FilesToExpand: []string{name}})
		if err != nil {
			t.Fatal(err)
		}
		expected := strings.Replace(name, "entry", "dep", 1)
		if _, ok := built.Graph.Reverse([]string{expected}, []Kind{Imports})[name]; !ok {
			t.Fatalf("wrong context: %+v", built)
		}
		if !slices.Contains(built.ParsedFiles, expected) || len(built.ParsedFiles) != 2 {
			t.Fatal(built.ParsedFiles)
		}
	}
}

func TestPythonSearchRootDoesNotImportAncestorPackages(t *testing.T) {
	built := pythonGraph(t, pythonPrelude+"sys.path.insert(0,str(ROOT / 'one' / 'src'))\nimport dep\n", map[string]string{
		"one/__init__.py": "", "one/src/__init__.py": "", "one/src/dep.py": "VALUE=1\n",
	})
	edges := pythonFileEdges(built)
	if len(edges) != 1 || edges[0].To != "one/src/dep.py" || edges[0].Confidence != "" {
		t.Fatalf("search root became package dependency: %+v", edges)
	}
}

func TestPythonUnrelatedControlFlowKeepsKnownPrefix(t *testing.T) {
	built := pythonGraph(t, pythonPrelude+`sys.path.insert(0,str(ROOT / 'one'))
for item in runtime_items:
    print(item)
try:
    print('value')
except Exception:
    pass
import dep
`, nil)
	edges := pythonFileEdges(built)
	if len(edges) != 1 || edges[0].To != "one/dep.py" || edges[0].Confidence != "" || len(built.Issues) != 0 {
		t.Fatalf("unrelated control flow changed import context: edges=%+v issues=%+v", edges, built.Issues)
	}
}

func TestPythonLambdaRetainsUnknownDependency(t *testing.T) {
	built := pythonGraph(t, "load = lambda name: __import__(name)\n", nil)
	if len(built.Issues) != 1 || built.Issues[0].Code != "dynamic_target" {
		t.Fatalf("lambda lost unresolved dependency: %+v", built.Issues)
	}
}

func TestPythonReadOnlySearchPathAndLargeDataDoNotCreateGaps(t *testing.T) {
	source := pythonPrelude + "sys.path.insert(0,str(ROOT / 'one'))\nindex=sys.path.index('value')\nDATA=[" + strings.Repeat("1,", 40) + "]\nimport dep\n"
	built := pythonGraph(t, source, nil)
	edges := pythonFileEdges(built)
	if len(built.Issues) != 0 || len(edges) != 1 || edges[0].Confidence != "" {
		t.Fatalf("%+v %+v", built.Issues, edges)
	}
}

func TestPythonExpressionDependenciesRemainVisible(t *testing.T) {
	for _, source := range []string{
		"@__import__(name)\ndef work(): pass\n",
		"def work(value=__import__(name)): pass\n",
		"value=f'{__import__(name)}'\n",
	} {
		built := pythonGraph(t, source, nil)
		found := false
		for _, issue := range built.Issues {
			if issue.Code == "dynamic_target" {
				found = true
			}
		}
		if !found {
			t.Fatalf("lost dependency in %q: %+v", source, built.Issues)
		}
	}
	built := pythonGraph(t, pythonPrelude+"sys.path.insert(0,str(ROOT / 'one'))\ndel sys.path[:]\nimport dep\n", nil)
	for _, edge := range pythonFileEdges(built) {
		if edge.Confidence == "" {
			t.Fatalf("deleted paths stayed definite: %+v", edge)
		}
	}
}
