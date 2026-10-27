package repocli_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	cg "github.com/compforge/codegraph"
	"github.com/compforge/repocli/toolkit/go"
)

// +case=component_tag_counts
func TestComponentTagCountsSeparateRootMetadataAndNestedSource(t *testing.T) {
	root := fixture(t)
	put(t, root, "web/package.json", `{"name":"web"}`)
	put(t, root, "web/main.ts", "export const value = 1;\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-qm", "nested component")
	put(t, root, "VERSION", "1.0.1\n")
	put(t, root, "notes.md", "release notes\n")
	for _, sourceChanged := range []bool{false, true} {
		if sourceChanged {
			put(t, root, "web/main.ts", "export const value = 2;\n")
		}
		report, err := repocli.AnalyzeImpact(context.Background(), repocli.DiffRequest{Repository: root})
		if err != nil {
			t.Fatal(err)
		}
		if report.Complete || len(report.Diagnostics) != 2 {
			t.Fatalf("missing resource gaps: %+v", report.Diagnostics)
		}
		for _, diagnostic := range report.Diagnostics {
			if diagnostic.Reason != "unsupported_resource_change" {
				t.Fatal(diagnostic)
			}
		}
		foundRoot, foundWeb := false, false
		for _, component := range report.Components {
			switch component.Root {
			case ".":
				foundRoot = true
				if !component.Affected || component.Complete || !reflect.DeepEqual(component.TagCounts, map[cg.Tag]int{cg.VersionTag: 1, cg.DocumentationTag: 1}) {
					t.Fatal(component)
				}
			case "web":
				foundWeb = true
				want := map[cg.Tag]int{}
				if sourceChanged {
					want[cg.SourceTag] = 1
				}
				if component.Affected != sourceChanged || !component.Complete || !reflect.DeepEqual(component.TagCounts, want) {
					t.Fatal(component)
				}
			}
			wire, err := json.Marshal(component)
			if err != nil {
				t.Fatal(err)
			}
			var decoded map[string]json.RawMessage
			if err := json.Unmarshal(wire, &decoded); err != nil {
				t.Fatal(err)
			}
			if counts, ok := decoded["tagCounts"]; !ok || string(counts) == "null" {
				t.Fatalf("missing count object: %s", wire)
			}
		}
		if !foundRoot || !foundWeb {
			t.Fatal(report.Components)
		}
	}
}

func TestComponentTagCountsRetainMetadataConsumers(t *testing.T) {
	for _, name := range []string{"guide.md", "VERSION"} {
		t.Run(name, func(t *testing.T) {
			root := fixture(t)
			put(t, root, "web/package.json", `{"name":"web"}`)
			put(t, root, "web/"+name, "old\n")
			put(t, root, "web/guide.test.ts", "import value from './"+name+"';\n")
			git(t, root, "add", ".")
			git(t, root, "commit", "-qm", "asset consumer")
			put(t, root, "web/"+name, "new\n")
			report, err := repocli.AnalyzeImpact(context.Background(), repocli.DiffRequest{Repository: root})
			if err != nil {
				t.Fatal(err)
			}
			for _, component := range report.Components {
				if component.Root != "web" {
					continue
				}
				tag := cg.DocumentationTag
				if name == "VERSION" {
					tag = cg.VersionTag
				}
				want := map[cg.Tag]int{tag: 1, cg.SourceTag: 1, cg.TestTag: 1}
				if component.Complete || !reflect.DeepEqual(component.TagCounts, want) || len(component.TestFiles) != 1 {
					t.Fatal(component)
				}
				return
			}
			t.Fatal("missing consumer component")
		})
	}
}

func TestComponentTagCountsDoNotHideUnknownOrConfigurationGaps(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		tag           cg.Tag
	}{
		{"asset.mystery", "unknown", cg.UnknownTag},
		{"tsconfig.json", `{}`, cg.ResourceTag},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := fixture(t)
			put(t, root, tc.name, tc.content)
			report, err := repocli.AnalyzeImpact(context.Background(), repocli.DiffRequest{Repository: root})
			if err != nil {
				t.Fatal(err)
			}
			for _, component := range report.Components {
				if component.Root != "." {
					continue
				}
				if !component.Affected || component.Complete || component.TagCounts[tc.tag] != 1 || len(component.FallbackReasons) == 0 {
					t.Fatal(component)
				}
				return
			}
			t.Fatal("missing root component")
		})
	}
}

// +case=overlapping_tags_preserve_configuration_gaps
func TestComponentTagCountsPreserveConfigurationGapsWithDocumentation(t *testing.T) {
	for _, tc := range []struct {
		name string
		tags map[cg.Tag]int
	}{
		{".env.local.txt", map[cg.Tag]int{cg.ConfigTag: 1, cg.DocumentationTag: 1}},
		{"requirements-custom.txt", map[cg.Tag]int{cg.DocumentationTag: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := fixture(t)
			put(t, root, tc.name, "old\n")
			git(t, root, "add", ".")
			git(t, root, "commit", "-qm", "configuration fixture")
			put(t, root, tc.name, "new\n")
			report, err := repocli.AnalyzeImpact(context.Background(), repocli.DiffRequest{Repository: root})
			if err != nil {
				t.Fatal(err)
			}
			if report.Complete || len(report.Diagnostics) != 1 || report.Diagnostics[0].Reason != "configuration_change" || report.Diagnostics[0].Path != tc.name {
				t.Fatalf("lost configuration gap: %+v", report.Diagnostics)
			}
			for _, component := range report.Components {
				if component.Root != "." {
					continue
				}
				if component.Complete || !component.Affected || !reflect.DeepEqual(component.TagCounts, tc.tags) || len(component.TestFiles) != 0 {
					t.Fatal(component)
				}
				return
			}
			t.Fatal("missing root component")
		})
	}
}
