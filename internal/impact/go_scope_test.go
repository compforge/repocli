package impact

import (
	"context"
	"maps"
	"reflect"
	"testing"

	"github.com/compforge/repocli/internal/diff"
)

func TestGoScopePreservesBeforeAndAfterConsumers(t *testing.T) {
	before := files(map[string]string{
		"go.mod":              "module example.com/app\n\ngo 1.25.0\n",
		"a/a.go":              "package a\nfunc Value() int { return 1 }\n",
		"a/a_test.go":         "package a\nvar _ = Value\n",
		"b/b.go":              "package b\nimport \"example.com/app/a\"\nvar _ = a.Value\n",
		"b/b_test.go":         "package b\nfunc TestB() {}\n",
		"other/other.go":      "package other\nfunc Value() int { return 1 }\n",
		"other/other_test.go": "package other\nvar _ = Value\n",
	})
	for _, status := range []string{"modified", "deleted", "renamed"} {
		t.Run(status, func(t *testing.T) {
			after := maps.Clone(before)
			// Simulate a removed import even if a changed-file filter keeps only a.
			after["b/b.go"] = []byte("package b\nvar Value = 1\n")
			change := diff.Change{Path: "a/a.go", Status: status}
			switch status {
			case "modified":
				after["a/a.go"] = []byte("package a\nfunc Value() int { return 2 }\n")
			case "deleted":
				delete(after, "a/a.go")
			case "renamed":
				delete(after, "a/a.go")
				change.Path, change.OldPath = "c/a.go", "a/a.go"
				after["c/a.go"] = []byte("package c\nfunc Value() int { return 1 }\n")
			}
			got, err := Analyze(context.Background(), Request{Before: before, After: after, Changes: []diff.Change{change}, TestDirs: []string{"."}, Mode: "file"})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.TestFiles, []string{"a/a_test.go", "b/b_test.go"}) {
				t.Fatalf("%+v", got)
			}
		})
	}
}

func TestGoScopeDoesNotParseUnrelatedBodies(t *testing.T) {
	snapshot := files(map[string]string{
		"go.mod":      "module example.com/app\n\ngo 1.25.0\n",
		"a/a.go":      "package a\nfunc Value() int { return 1 }\n",
		"a/a_test.go": "package a\nvar _ = Value\n",
		"b/b.go":      "package b\nfunc Broken() { (((\n",
		"b/b_test.go": "package b\nfunc BrokenTest() { (((\n",
	})
	got, err := Analyze(context.Background(), Request{Before: snapshot, After: snapshot, Changes: []diff.Change{{Path: "a/a.go", Status: "modified"}}, TestDirs: []string{"."}, Mode: "file"})
	if err != nil || got.Scope != "focused" || !reflect.DeepEqual(got.TestFiles, []string{"a/a_test.go"}) {
		t.Fatalf("%+v %v", got, err)
	}
	if len(got.Observations) != 0 || len(got.Uncertainties) != 0 {
		t.Fatalf("parsed unrelated bodies: %+v", got)
	}
}
