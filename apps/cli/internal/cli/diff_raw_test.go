package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/compforge/repocli/toolkit/go"
)

func TestDiffCommandShowsPatchesWithoutImpact(t *testing.T) {
	root := t.TempDir()
	gitCommand(t, root, "init", "-q")
	put(t, root, "a.go", "package p\nfunc A(){}\n")
	gitCommand(t, root, "add", ".")
	gitCommand(t, root, "commit", "-qm", "base")
	put(t, root, "a.go", "package p\nfunc B(){}\n")
	var out, errout bytes.Buffer
	if code := Execute(context.Background(), []string{"diff", "--repo", root, "--json"}, nil, &out, &errout); code != 0 {
		t.Fatal(code, errout.String())
	}
	var d repocli.DiffReport
	if err := json.Unmarshal(out.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Changes) != 1 || d.Changes[0].Diff == "" {
		t.Fatalf("%+v", d)
	}
	var wire map[string]any
	_ = json.Unmarshal(out.Bytes(), &wire)
	if _, ok := wire["affectedFiles"]; ok {
		t.Fatal("diff performed impact analysis")
	}
}
