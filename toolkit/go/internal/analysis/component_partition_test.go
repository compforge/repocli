package analysis

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	cg "github.com/compforge/codegraph"
	"github.com/compforge/repocli/toolkit/go/internal/diff"
	"github.com/compforge/repocli/toolkit/go/internal/impact"
	"github.com/compforge/repocli/toolkit/go/internal/project"
)

// +case=component_impact_partition
func TestComponentAndUnownedPartitionKeepsNestedOwnershipAndEvidence(t *testing.T) {
	layout, _ := project.Load(map[string][]byte{"app/Makefile": nil, "app/child/Makefile": nil}, "")
	c := &comparison{changes: []diff.Change{
		{Path: "app/code.py", Status: "modified"},
		{Path: "app/child/code.py", Status: "added"},
		{Path: "shared.cfg", Status: "modified"},
	}}
	result := impact.Result{
		SourceFiles: []string{"app/code.py", "app/child/code.py"},
		TestFiles:   []string{"tests/test_shared.py", "app/child/test_code.py", "tests/test_shared.py"},
		AffectedFiles: []impact.FileImpact{
			{Path: "tests/test_shared.py", Version: "after"}, {Path: "tests/test_shared.py", Version: "before"},
			{Path: "app/child/test_code.py", Version: "after"}, {Path: "app/code.py", Version: "after"},
		},
		Uncertainties: []impact.Uncertainty{{Path: "shared.cfg", Version: "after", Message: "unknown configuration", Scope: "dependency"}},
	}
	groups, unowned := componentResults(layout, layout, c, result, nil)
	if len(groups) != 2 || groups[0].Root != "app" || groups[1].Root != "app/child" {
		t.Fatal(groups)
	}
	if !slices.Equal(groups[0].ChangedFiles, []string{"app/code.py"}) || len(groups[0].TestFiles) != 0 {
		t.Fatal(groups[0])
	}
	if !slices.Equal(groups[1].TestFiles, []string{"app/child/test_code.py"}) || !groups[1].Complete {
		t.Fatal(groups[1])
	}
	if !unowned.Affected || unowned.Complete || !slices.Equal(unowned.ChangedFiles, []string{"shared.cfg"}) ||
		!slices.Equal(unowned.TestFiles, []string{"tests/test_shared.py"}) || unowned.AffectedFileCounts[cg.TestTag] != 1 {
		t.Fatal(unowned)
	}
	// Output ordering is stable even if layout enumeration changes.
	slices.Reverse(layout.Components)
	again, remaining := componentResults(layout, layout, c, result, nil)
	if !reflect.DeepEqual(groups, again) || !reflect.DeepEqual(unowned, remaining) {
		t.Fatal(again, remaining)
	}
}

func TestComponentMovesToUnownedAndRetainsBeforeSourcePath(t *testing.T) {
	before, _ := project.Load(map[string][]byte{"old/Makefile": nil}, "")
	after := project.Layout{}
	c := &comparison{changes: []diff.Change{{Path: "new/code.py", OldPath: "old/code.py", Status: "renamed"}}}
	groups, unowned := componentResults(before, after, c, impact.Result{SourceFiles: []string{"new/code.py"}}, nil)
	if len(groups) != 1 || groups[0].Snapshot != "before" || !slices.Equal(groups[0].SourceFiles, []string{"old/code.py"}) {
		t.Fatal(groups)
	}
	if !slices.Equal(unowned.SourceFiles, []string{"new/code.py"}) || unowned.AffectedFileCounts[cg.SourceTag] != 1 {
		t.Fatal(unowned)
	}
}

func TestComponentNameChangeAtSameRootRetainsTwoIdentities(t *testing.T) {
	before, _ := project.Load(map[string][]byte{"app/Makefile": nil}, "")
	after, _ := project.Load(map[string][]byte{"app/Makefile": nil}, "")
	before.Components[0].Name = "former"
	c := &comparison{changes: []diff.Change{{Path: "app/code.py", Status: "modified"}}}
	groups, _ := componentResults(before, after, c, impact.Result{}, nil)
	if len(groups) != 2 || groups[0].Snapshot != "after" || groups[1].Snapshot != "before" || groups[1].Component.Name != "former" {
		t.Fatal(groups)
	}
}

func TestComponentLanguageMetadataChangeRetainsOneIdentity(t *testing.T) {
	before, _ := project.Load(map[string][]byte{"app/package.json": []byte(`{}`)}, "")
	after, _ := project.Load(map[string][]byte{"app/package.json": []byte(`{"devDependencies":{"typescript":"*"}}`)}, "")
	c := &comparison{changes: []diff.Change{{Path: "app/code.js", Status: "deleted"}}}
	groups, _ := componentResults(before, after, c, impact.Result{}, nil)
	if len(groups) != 1 || groups[0].Component.Language != "typescript" || groups[0].AffectedFileCounts[cg.SourceTag] != 1 {
		t.Fatal(groups)
	}
}

func TestVersionedGapDoesNotLeakToNewOwner(t *testing.T) {
	before, _ := project.Load(map[string][]byte{"app/Makefile": nil}, "")
	after, _ := project.Load(map[string][]byte{"app/Makefile": nil, "app/child/Makefile": nil}, "")
	result := impact.Result{Uncertainties: []impact.Uncertainty{{Path: "app/child/a.py", Version: "before", Message: "old dependency gap"}}}
	groups, unowned := componentResults(before, after, &comparison{}, result, nil)
	if groups[0].Complete || !groups[1].Complete || !unowned.Complete {
		t.Fatal(groups, unowned)
	}
}

func TestRepositoryGapCoversLateBeforeOnlyBuckets(t *testing.T) {
	before, _ := project.Load(map[string][]byte{"old/Makefile": nil}, "")
	result := impact.Result{Uncertainties: []impact.Uncertainty{
		{Scope: "repository", Message: "capture gap"},
		{Path: "old/code.py", Version: "before", Message: "old gap"},
	}}
	groups, unowned := componentResults(before, project.Layout{}, &comparison{}, result, nil)
	if len(groups) != 1 || groups[0].Complete || unowned.Complete || !slices.Contains(groups[0].FallbackReasons, "capture gap") {
		t.Fatal(groups, unowned)
	}
}

func TestEmptyImpactUsesListAndUnownedObjectWithoutSyntheticComponent(t *testing.T) {
	groups, unowned := componentResults(project.Layout{}, project.Layout{}, &comparison{}, impact.Result{}, nil)
	data, err := json.Marshal(struct {
		Components []project.ComponentImpact `json:"components"`
		Unowned    project.ImpactSummary     `json:"unowned"`
	}{groups, unowned})
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if string(wire["components"]) != "[]" || string(wire["unowned"]) == "null" || !unowned.Complete || unowned.Affected {
		t.Fatal(string(data))
	}
}
