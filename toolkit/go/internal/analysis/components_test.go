package analysis

import (
	"slices"
	"testing"

	"github.com/compforge/repocli/toolkit/go/internal/diff"
	"github.com/compforge/repocli/toolkit/go/internal/impact"
	"github.com/compforge/repocli/toolkit/go/internal/project"
)

func TestComponentImpactIncludesChangesAndReachableFilesWithoutTests(t *testing.T) {
	layout, err := project.Load(map[string][]byte{"lib/go.mod": nil, "app/go.mod": nil, "other/go.mod": nil}, "")
	if err != nil {
		t.Fatal(err)
	}
	result := impact.Result{AffectedFiles: []impact.FileImpact{{Path: "app/use.go", Version: "after"}, {Path: "app/use.go", Version: "before"}}}
	groups := componentResults(layout, layout, []diff.Change{{Path: "lib/config.json", Status: "modified"}}, result, nil)
	for _, g := range groups {
		switch g.Root {
		case "lib":
			if !g.Affected || !slices.Equal(g.ChangedFiles, []string{"lib/config.json"}) {
				t.Fatal(g)
			}
		case "app":
			if !g.Affected || !slices.Equal(g.AffectedFiles, []string{"app/use.go"}) || len(g.TestFiles) != 0 {
				t.Fatal(g)
			}
		case "other":
			if g.Affected || !g.Complete {
				t.Fatal(g)
			}
		}
	}
}

func TestComponentImpactRetainsRenameAndDeletedOwnership(t *testing.T) {
	before, _ := project.Load(map[string][]byte{"old/go.mod": nil, "app/go.mod": nil}, "")
	after, _ := project.Load(map[string][]byte{"new/go.mod": nil, "app/go.mod": nil}, "")
	groups := componentResults(before, after, []diff.Change{{Path: "new/data.json", OldPath: "old/data.json", Status: "renamed"}}, impact.Result{}, nil)
	for _, g := range groups {
		if g.Root == "app" {
			if g.Affected {
				t.Fatal(g)
			}
			continue
		}
		if !g.Affected || !slices.Equal(g.ChangedFiles, []string{g.Root + "/data.json"}) {
			t.Fatal(g)
		}
		if g.Root == "old" && g.Snapshot != "before" {
			t.Fatal(g)
		}
	}
}

func TestUnknownComponentImpactIsNotClaimedUnaffected(t *testing.T) {
	layout, _ := project.Load(map[string][]byte{"lib/go.mod": nil, "other/go.mod": nil}, "")
	for _, scope := range []string{"component", "repository"} {
		result := impact.Result{Uncertainties: []impact.Uncertainty{{Scope: scope, Path: "lib/config.json", Message: "unknown configuration effect"}}}
		groups := componentResults(layout, layout, nil, result, nil)
		for _, g := range groups {
			unknown := scope == "repository" || g.Root == "lib"
			if g.Affected || g.Complete == unknown || (len(g.FallbackReasons) > 0) != unknown {
				t.Fatal(g)
			}
		}
	}
}
