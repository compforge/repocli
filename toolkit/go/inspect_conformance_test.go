package repocli_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/compforge/repocli/toolkit/go"
)

// Both language implementations materialize this exact input transition. In
// particular, metadata from the working tree cannot leak into index/commit reads.
func TestInspectVersionConformance(t *testing.T) {
	data, err := os.ReadFile("../../conformance/inspect/versions.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Commit   map[string]string `json:"commit"`
		Index    map[string]string `json:"index"`
		Working  map[string]string `json:"working_tree"`
		Expected map[string]string `json:"expected"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	write := func(files map[string]string) {
		t.Helper()
		for name, text := range files {
			path := filepath.Join(root, name)
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(text), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	git("init", "-q")
	write(fixture.Commit)
	git("add", ".")
	git("-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-qm", "fixture")
	write(fixture.Index)
	git("add", ".")
	write(fixture.Working)
	for _, input := range []string{"commit", "index", "working_tree"} {
		t.Run(input, func(t *testing.T) {
			request := repocli.InputRequest{Repository: root, Staged: input == "index"}
			if input == "commit" {
				request.Head = "HEAD"
			}
			report, err := repocli.Inspect(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			if report.Input != input || !report.Complete || len(report.Diagnostics) != 0 || len(report.Components) != 1 || report.Components[0].Name != fixture.Expected[input] {
				t.Fatalf("unexpected report: %+v", report)
			}
			if input == "commit" && report.Head == "" {
				t.Fatal("commit input must be resolved")
			}
		})
	}
}
