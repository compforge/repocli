package repocli_test

import (
	"context"
	cg "github.com/compforge/codegraph"
	"github.com/compforge/repocli/toolkit/go"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// +case:id=captured_layout,desc=`Component manifests move and the checkout changes after capture`,expect=`Both layouts keep their captured ownership and identity without filesystem access`
func TestDiffInspectRetainsCapturedLayouts(t *testing.T) {
	root := fixture(t)
	git(t, root, "remote", "add", "origin", "https://github.com/example/original.git")
	put(t, root, "old/pyproject.toml", "[project]\nname = 'old'\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-qm", "old component")
	if err := os.Rename(filepath.Join(root, "old"), filepath.Join(root, "new")); err != nil {
		t.Fatal(err)
	}
	put(t, root, "new/app.py", "print('new')\n")
	put(t, root, "new/uv.lock", "version = 1\n")
	d, err := repocli.Diff(context.Background(), repocli.DiffRequest{Repository: root})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Complete {
		t.Fatalf("capture incomplete: %+v", d.Diagnostics)
	}
	for _, ch := range d.Changes {
		if ch.Path() == "new/uv.lock" && !slices.Contains(ch.Tags, cg.LockfileTag) {
			t.Fatal("lock tag missing", ch.Tags)
		}
	}
	git(t, root, "remote", "set-url", "origin", "https://github.com/example/later.git")
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	d = d.Select("new/app.py")
	for _, tc := range []struct {
		before         bool
		root, snapshot string
	}{{true, "old", d.BeforeSnapshot}, {false, "new", d.AfterSnapshot}} {
		report, err := d.Inspect(tc.before)
		if err != nil {
			t.Fatal(err)
		}
		if !report.Complete || report.Snapshot != tc.snapshot || report.Repository.Path != "example/original" {
			t.Fatalf("report: %+v", report)
		}
		owner := report.Owner(tc.root + "/app.py")
		if owner == nil || owner.Root != tc.root || owner.Language != "python" {
			t.Fatalf("owner: %+v", owner)
		}
		other := "new"
		if !tc.before {
			other = "old"
		}
		if owner := report.Owner(other + "/app.py"); owner == nil || owner.Root != "." {
			t.Fatalf("invented other-side component: %+v", owner)
		}
	}
	if _, err := (repocli.DiffReport{}).Inspect(false); err == nil {
		t.Fatal("missing capture accepted")
	}
}

func TestTreeUsesSharedLockfileClassification(t *testing.T) {
	root := fixture(t)
	put(t, root, "pdm.lock", "version = 1\n")
	put(t, root, "pylock.dev.toml", "lock-version = '1.0'\n")
	tree, err := repocli.Tree(context.Background(), repocli.InputRequest{Repository: root})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, file := range tree.Files {
		if file.Path == "pdm.lock" || file.Path == "pylock.dev.toml" {
			count++
			if file.Role != "lockfile" {
				t.Fatal(file)
			}
		}
	}
	if count != 2 {
		t.Fatal(count)
	}
}

func TestDiffInspectSelectedInput(t *testing.T) {
	for _, input := range []string{"index", "commit", "patch"} {
		t.Run(input, func(t *testing.T) {
			root := fixture(t)
			req := repocli.DiffRequest{Repository: root}
			switch input {
			case "index":
				put(t, root, "web/package.json", `{"devDependencies":{"typescript":"*"}}`)
				git(t, root, "add", ".")
				req.Staged = true
				if err := os.RemoveAll(filepath.Join(root, "web")); err != nil {
					t.Fatal(err)
				}
			case "commit":
				put(t, root, "web/package.json", `{"devDependencies":{"typescript":"*"}}`)
				git(t, root, "add", ".")
				git(t, root, "commit", "-qm", "web")
				req.Base, req.Head = "HEAD~1", "HEAD"
				if err := os.RemoveAll(filepath.Join(root, "web")); err != nil {
					t.Fatal(err)
				}
			case "patch":
				req.PatchFile = "-"
				req.Stdin = strings.NewReader("diff --git a/web/package.json b/web/package.json\nnew file mode 100644\n--- /dev/null\n+++ b/web/package.json\n@@ -0,0 +1 @@\n+{\"devDependencies\":{\"typescript\":\"*\"}}\n")
			}
			d, err := repocli.Diff(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			after, err := d.Inspect(false)
			if err != nil {
				t.Fatal(err)
			}
			if owner := after.Owner("web/app.ts"); owner == nil || owner.Root != "web" || owner.Language != "typescript" || after.Input != input || after.Snapshot != d.AfterSnapshot {
				t.Fatalf("after: %+v", after)
			}
			before, err := d.Inspect(true)
			if err != nil {
				t.Fatal(err)
			}
			if owner := before.Owner("web/app.ts"); owner == nil || owner.Root != "." {
				t.Fatalf("before: %+v", before)
			}
		})
	}
}
