package git_test

import (
	"context"
	"strings"
	"testing"

	repogit "github.com/compforge/repocli/toolkit/go/internal/git"
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
}
