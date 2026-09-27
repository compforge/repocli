package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/compforge/repocli/internal/analysis"
	"github.com/compforge/repocli/internal/project"
)

func setProject(t *testing.T, dir, name, manager string) {
	t.Helper()
	put(t, dir, ".repocli.json", `{"repository":{"forge":{"name":"github"},"path":"example/`+name+`"},"components":[{"name":"`+name+`","root":".","language":"typescript"}]}`)
	put(t, dir, "package.json", `{"packageManager":"`+manager+`@1.0.0"}`)
}

// +case=`Repository identity, ownership and package tools follow the selected input, including patch postimages`
func TestPrepareContextAcrossCommandsAndVersions(t *testing.T) {
	dir := fixture(t)
	setProject(t, dir, "committed", "npm")
	gitCommand(t, dir, "add", ".")
	gitCommand(t, dir, "commit", "-qm", "project metadata")
	setProject(t, dir, "staged", "yarn")
	gitCommand(t, dir, "add", ".")
	setProject(t, dir, "working", "pnpm")
	for _, tc := range []struct {
		name, manager string
		flags         []string
	}{
		{"committed", "npm", []string{"--head", "HEAD"}},
		{"staged", "yarn", []string{"--staged"}},
		{"working", "pnpm", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := snapshotJSON(t, dir, tc.flags...)
			if snapshot.Repository == nil || snapshot.Repository.Path != "example/"+tc.name || len(snapshot.Components) != 1 {
				t.Fatalf("wrong context: %+v", snapshot)
			}
			component := snapshot.Components[0]
			if component.Name != tc.name || component.Language != "typescript" || len(component.PackageTools) != 1 || component.PackageTools[0].Name != tc.manager {
				t.Fatalf("wrong component: %+v", component)
			}
			report := runJSON(t, append([]string{"diff", "--repo", dir, "--json"}, tc.flags...), "")
			if !reflect.DeepEqual(report.Repository, snapshot.Repository) || report.Snapshot != snapshot.Snapshot {
				t.Fatalf("diff/snapshot mismatch: %+v / %+v", report, snapshot)
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
	snapshot := snapshotJSON(t, dir)
	graph, err := analysis.CaptureGraph(context.Background(), analysis.SnapshotRequest{Repository: dir}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(graph.Snapshot.Layout, snapshot.Layout) || graph.Snapshot.Snapshot != snapshot.Snapshot {
		t.Fatalf("graph context must precede graph limits: %+v", graph.Snapshot)
	}
	patch := gitCommand(t, dir, "diff", "--binary", "HEAD")
	setProject(t, dir, "unrelated-live", "bun")
	report := runJSON(t, []string{"diff", "--repo", dir, "--file", "-", "--json"}, patch)
	if report.Repository.Path != "example/working" || report.Snapshot != snapshot.Snapshot {
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
	snapshot := snapshotJSON(t, dir)
	if snapshot.Repository != nil {
		t.Fatal("invented repository identity")
	}
	got := map[string]string{}
	for _, c := range snapshot.Components {
		got[c.Root] = c.Language
	}
	want := map[string]string{"api": "go", "web": "typescript", "worker": "python"}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	if owner := snapshot.Owner("api/main.go"); owner == nil || owner.Name != "api" {
		t.Fatal(owner)
	}
	if snapshot.Owner("shared.txt") != nil {
		t.Fatal("invented shared root owner")
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip struct {
		Components []project.Binding `json:"components"`
	}
	if err := json.Unmarshal(raw, &roundtrip); err != nil || len(roundtrip.Components) != 3 {
		t.Fatal(string(raw), err)
	}
	var out, stderr bytes.Buffer
	if code := Execute(context.Background(), []string{"snapshot", "--repo", dir}, nil, &out, &stderr); code != 0 || !strings.Contains(out.String(), "package tool pnpm 10.0.0") {
		t.Fatal(code, out.String(), stderr.String())
	}
}

func TestPrepareInvalidMetadataAndInformationalCommands(t *testing.T) {
	dir := fixture(t)
	put(t, dir, ".repocli.json", `{`)
	for _, command := range []string{"snapshot", "diff", "view"} {
		var out, stderr bytes.Buffer
		args := []string{command, "--repo", dir}
		if command == "view" {
			args = append(args, "--addr", "127.0.0.1:0")
		}
		if code := Execute(context.Background(), args, nil, &out, &stderr); code != 1 || !strings.Contains(stderr.String(), ".repocli.json") {
			t.Fatal(command, code, stderr.String())
		}
	}
	for _, command := range []string{"version", "help"} {
		var out, stderr bytes.Buffer
		if code := Execute(context.Background(), []string{command, "--repo", t.TempDir()}, nil, &out, &stderr); code != 0 {
			t.Fatal(command, code, stderr.String())
		}
	}
}
