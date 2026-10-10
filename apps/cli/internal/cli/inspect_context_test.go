package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/compforge/repocli/toolkit/go"
)

func setProject(t *testing.T, dir, manager string) {
	t.Helper()
	put(t, dir, "package.json", `{"devDependencies":{"typescript":"*"},"packageManager":"`+manager+`@1.0.0"}`)
}

// +case=`Ownership and package tools follow the selected input, including patch postimages`
func TestPrepareContextAcrossCommandsAndVersions(t *testing.T) {
	dir := fixture(t)
	gitCommand(t, dir, "remote", "add", "origin", "https://github.com/example/repo.git")
	setProject(t, dir, "npm")
	gitCommand(t, dir, "add", ".")
	gitCommand(t, dir, "commit", "-qm", "project metadata")
	setProject(t, dir, "yarn")
	gitCommand(t, dir, "add", ".")
	setProject(t, dir, "pnpm")
	for _, tc := range []struct {
		name, manager string
		flags         []string
	}{
		{"committed", "npm", []string{"--head", "HEAD"}},
		{"staged", "yarn", []string{"--staged"}},
		{"working", "pnpm", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inspection := inspectJSON(t, dir, tc.flags...)
			if inspection.Repository == nil || inspection.Repository.Path != "example/repo" || len(inspection.Components) != 1 {
				t.Fatalf("wrong context: %+v", inspection)
			}
			component := inspection.Components[0]
			if component.Name != "." || component.Language != "typescript" || len(component.PackageTools) != 1 || component.PackageTools[0].Name != tc.manager {
				t.Fatalf("wrong component: %+v", component)
			}
			report := runJSON(t, append([]string{"impact", "--repo", dir, "--json"}, tc.flags...), "")
			if !reflect.DeepEqual(report.Repository, inspection.Repository) || report.Snapshot != snapshotJSON(t, dir, tc.flags...).Snapshot {
				t.Fatalf("diff/snapshot mismatch: %+v / %+v", report, inspection)
			}
			found := false
			for _, group := range report.Components {
				if group.Snapshot == "after" {
					found = true
					if group.Component != component.Component || !reflect.DeepEqual(group.PackageTools, component.PackageTools) {
						t.Fatalf("diff component drift: %+v", group)
					}
				}
			}
			if !found {
				t.Fatal("missing current component catalog")
			}
		})
	}
	inspection := inspectJSON(t, dir)
	graph, err := repocli.Graph(context.Background(), repocli.InputRequest{Repository: dir}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(graph.Layout, inspection.Layout) || graph.Snapshot.Snapshot != snapshotJSON(t, dir).Snapshot {
		t.Fatalf("graph context must precede graph limits: %+v", graph.Snapshot)
	}
	digest := snapshotJSON(t, dir).Snapshot
	patch := gitCommand(t, dir, "diff", "--binary", "HEAD")
	setProject(t, dir, "bun")
	report := runJSON(t, []string{"impact", "--repo", dir, "--file", "-", "--json"}, patch)
	if report.Repository.Path != "example/repo" || report.Snapshot != digest {
		t.Fatalf("patch used live metadata: %+v", report)
	}
}

func TestPrepareDiscoveryWithoutChanges(t *testing.T) {
	dir := fixture(t)
	put(t, dir, "api/go.mod", "module example/api\n")
	put(t, dir, "web/package.json", `{"devDependencies":{"typescript":"*"},"packageManager":"pnpm@10.0.0"}`)
	put(t, dir, "worker/pyproject.toml", "[project]\nname = 'worker'\n")
	put(t, dir, "worker/uv.lock", "version = 1\n")
	gitCommand(t, dir, "add", ".")
	gitCommand(t, dir, "commit", "-qm", "components")
	inspection := inspectJSON(t, dir)
	if inspection.Repository != nil {
		t.Fatal("invented repository identity")
	}
	got := map[string]string{}
	for _, c := range inspection.Components {
		got[c.Root] = c.Language
	}
	want := map[string]string{"api": "go", "web": "typescript", "worker": "python"}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	if owner := inspection.Owner("api/main.go"); owner == nil || owner.Name != "api" {
		t.Fatal(owner)
	}
	if inspection.Owner("shared.txt") != nil {
		t.Fatal("invented shared root owner")
	}
	raw, err := json.Marshal(inspection)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip struct {
		Components []repocli.ComponentBinding `json:"components"`
	}
	if err := json.Unmarshal(raw, &roundtrip); err != nil || len(roundtrip.Components) != 3 {
		t.Fatal(string(raw), err)
	}
	var out, stderr bytes.Buffer
	if code := Execute(context.Background(), []string{"inspect", "--repo", dir}, nil, &out, &stderr); code != 0 || !strings.Contains(out.String(), "package tool pnpm 10.0.0") {
		t.Fatal(code, out.String(), stderr.String())
	}
}

func TestPrepareIgnoresLegacyConfiguration(t *testing.T) {
	dir := fixture(t)
	put(t, dir, ".repocli.json", `{`)
	for _, command := range []string{"inspect", "impact"} {
		var out, stderr bytes.Buffer
		if code := Execute(context.Background(), []string{command, "--repo", dir}, nil, &out, &stderr); code != 0 {
			t.Fatal(command, code, stderr.String())
		}
	}
	if _, err := repocli.Graph(context.Background(), repocli.InputRequest{Repository: dir}, 100); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"version", "help"} {
		var out, stderr bytes.Buffer
		if code := Execute(context.Background(), []string{command, "--repo", t.TempDir()}, nil, &out, &stderr); code != 0 {
			t.Fatal(command, code, stderr.String())
		}
	}
}

// +case=`Makefile-only Components retain unknown language and own changes across input versions`
func TestMakefileComponentImpactAndVersionSelection(t *testing.T) {
	dir := fixture(t)
	// A Makefile is an operation boundary, not code that inspection may execute.
	put(t, dir, "quality/Makefile", "$(error must not execute during analysis)\n")
	put(t, dir, "quality/check.py", "def check():\n    return 1\n")
	gitCommand(t, dir, "add", ".")
	gitCommand(t, dir, "commit", "-qm", "Makefile component")
	put(t, dir, "quality/check.py", "def check():\n    return 2\n")
	inspection := inspectJSON(t, dir)
	if len(inspection.Components) != 1 || inspection.Components[0].Root != "quality" || inspection.Components[0].Language != "" {
		t.Fatalf("unexpected Makefile layout: %+v", inspection.Layout)
	}
	report := runJSON(t, []string{"impact", "--repo", dir, "--json"}, "")
	if len(report.Components) != 1 || report.Components[0].Component.Name != "quality" || report.Components[0].Component.Language != "" || !reflect.DeepEqual(report.Components[0].SourceFiles, []string{"quality/check.py"}) {
		t.Fatalf("missing component impact: %+v", report.Components)
	}
	gitCommand(t, dir, "rm", "quality/Makefile")
	if len(inspectJSON(t, dir).Components) != 0 || len(inspectJSON(t, dir, "--staged").Components) != 0 || len(inspectJSON(t, dir, "--head", "HEAD").Components) != 1 {
		t.Fatal("Makefile discovery did not follow the selected input")
	}
	report = runJSON(t, []string{"impact", "--repo", dir, "--json"}, "")
	if len(report.Components) != 1 || report.Components[0].Snapshot != "before" || report.Components[0].Component.Name != "quality" {
		t.Fatalf("lost removed component identity: %+v", report.Components)
	}
}
