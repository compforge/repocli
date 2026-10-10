package repocli_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/compforge/repocli/toolkit/go"
)

func TestTreeSharedContract(t *testing.T) {
	data, err := os.ReadFile("../../conformance/tree/entries.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Files       map[string]string
		Symlinks    map[string]string
		Directories []string
		Roles       map[string]string
		Components  []string
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	git(t, root, "init", "-q")
	for path, data := range fixture.Files {
		put(t, root, path, data)
	}
	for path, target := range fixture.Symlinks {
		if err = os.Symlink(target, filepath.Join(root, path)); err != nil {
			t.Fatal(err)
		}
	}
	git(t, root, "add", ".")
	git(t, root, "commit", "-qm", "fixture")
	for _, req := range []repocli.InputRequest{{Repository: root}, {Repository: root, Staged: true}, {Repository: root, Head: "HEAD"}} {
		report, err := repocli.Tree(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		var dirs []string
		for _, dir := range report.Directories {
			dirs = append(dirs, dir.Path)
		}
		roles := map[string]string{}
		for _, file := range report.Files {
			if file.Role != "" {
				roles[file.Path] = file.Role
			}
			if file.Path == "go.mod" && (file.OID != "") != (req.Staged || req.Head != "") {
				t.Fatal(file)
			}
		}
		if !report.Complete || !reflect.DeepEqual(dirs, fixture.Directories) || !reflect.DeepEqual(roles, fixture.Roles) {
			t.Fatal(report)
		}
		layout, err := repocli.Inspect(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		var roots []string
		for _, component := range layout.Components {
			roots = append(roots, component.Root)
		}
		if !reflect.DeepEqual(roots, fixture.Components) {
			t.Fatal(layout)
		}
	}
	put(t, root, ".repocli.json", "{")
	if _, err = repocli.Tree(context.Background(), repocli.InputRequest{Repository: root}); err != nil {
		t.Fatal(err)
	}
	if _, err = repocli.Inspect(context.Background(), repocli.InputRequest{Repository: root}); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoveryLiteralPathAndFailure(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "repo \n")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	git(t, root, "init", "-q")
	put(t, root, "docs/readme.md", "hi")
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := repocli.FindCheckout(context.Background(), filepath.Join(root, "docs"))
	if err != nil || got != want {
		t.Fatal(got, err)
	}
	tree, err := repocli.Tree(context.Background(), repocli.InputRequest{Repository: root})
	if err != nil || tree.Checkout != want {
		t.Fatal(tree, err)
	}
	absent, err := repocli.FindCheckout(context.Background(), parent)
	if err != nil || absent != "" {
		t.Fatal(absent, err)
	}
	put(t, root, ".git/HEAD", "broken")
	if _, err = repocli.FindCheckout(context.Background(), root); err == nil {
		t.Fatal("corrupt metadata treated as absent")
	}
}
