package cmd

import (
	"context"
	"strings"
	"testing"

	repogit "github.com/compforge/repocli/internal/git"
)

func TestConfigResourcesRespectSnapshotVersion(t *testing.T) {
	parent, child := submoduleFixture(t)
	committed := `{"compilerOptions":{"strict":true}}`
	put(t, child, "tsconfig.json", committed)
	gitCommand(t, child, "add", "tsconfig.json")
	gitCommand(t, child, "commit", "-qm", "config")
	put(t, parent, "tsconfig.json", `{"extends":"./child/tsconfig"}`)
	put(t, parent, "tests/other.test.ts", "export const other=1;\n")
	gitCommand(t, parent, "add", "child", "tsconfig.json", "tests/other.test.ts")
	gitCommand(t, parent, "commit", "-qm", "config consumer")
	repo, err := repogit.Open(context.Background(), parent)
	if err != nil {
		t.Fatal(err)
	}
	dirty := `{"compilerOptions":{"paths":{"alias":["./target"]}}}`
	put(t, child, "tsconfig.json", dirty)
	for _, mode := range []string{"base", "index", "working"} {
		var snapshot repogit.Snapshot
		switch mode {
		case "base":
			snapshot, err = repo.Base(context.Background(), "HEAD")
		case "index":
			snapshot, err = repo.Staged(context.Background())
		case "working":
			snapshot, _, err = repo.Working(context.Background())
		}
		if err != nil {
			t.Fatal(err)
		}
		want := committed
		if mode == "working" {
			want = dirty
		}
		if string(snapshot.Resources["child/tsconfig.json"]) != want {
			t.Fatalf("%s: %+v", mode, snapshot.Resources)
		}
		for name := range snapshot.Files {
			if strings.HasPrefix(name, "child/") {
				t.Fatalf("child source escaped resource boundary: %s", name)
			}
		}
	}
	put(t, parent, "source file.ts", "export function a(){return 8;}\n")
	gitCommand(t, parent, "add", "source file.ts")
	staged := runJSON(t, []string{"diff", "--repo", parent, "--staged", "--test-dir", "tests", "--json"}, "")
	if !staged.Complete {
		t.Fatalf("index config came from dirty child: %+v", staged)
	}
	working := runJSON(t, []string{"diff", "--repo", parent, "--test-dir", "tests", "--json"}, "")
	if working.Complete {
		t.Fatal("changed unsupported config was hidden")
	}
	missing := false
	changed := false
	for _, d := range working.Diagnostics {
		missing = missing || d.Reason == "missing_config"
		changed = changed || d.Reason == "configuration_change"
	}
	if missing || !changed {
		t.Fatalf("wrong config diagnostic: %+v", working.Diagnostics)
	}
	for _, name := range working.TestFiles {
		if strings.HasPrefix(name, "child/") {
			t.Fatal("selected dependency tests")
		}
	}
	for _, component := range working.Components {
		if strings.HasPrefix(component.Root, "child") {
			t.Fatal("discovered child component")
		}
	}
}
