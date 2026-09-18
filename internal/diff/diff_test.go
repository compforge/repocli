package diff

import (
	"strings"
	"testing"
)

func TestContextIsNotChanged(t *testing.T) {
	patch := "diff --git a/a.ts b/a.ts\n--- a/a.ts\n+++ b/a.ts\n@@ -1,3 +1,3 @@\n const untouched = 0;\n-export const a = 1;\n+export const a = 2;\n const alsoUntouched = 3;\n"
	changes, err := Parse(strings.NewReader(patch))
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || len(changes[0].Hunks) != 1 || changes[0].Hunks[0] != (Hunk{Old: Range{2, 1}, New: Range{2, 1}}) {
		t.Fatalf("unexpected changes: %+v", changes)
	}
	after, err := Apply(map[string][]byte{"a.ts": []byte("const untouched = 0;\nexport const a = 1;\nconst alsoUntouched = 3;\n")}, changes)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after["a.ts"]), "a = 2") {
		t.Fatal("patch not applied")
	}
}

func TestRenameAndDelete(t *testing.T) {
	patch := "diff --git a/old.py b/new.py\nsimilarity index 100%\nrename from old.py\nrename to new.py\ndiff --git a/gone.py b/gone.py\ndeleted file mode 100644\n--- a/gone.py\n+++ /dev/null\n@@ -1 +0,0 @@\n-gone\n"
	changes, err := Parse(strings.NewReader(patch))
	if err != nil {
		t.Fatal(err)
	}
	base := map[string][]byte{"old.py": []byte("old\n"), "gone.py": []byte("gone\n")}
	after, err := Apply(base, changes)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 || string(after["new.py"]) != "old\n" {
		t.Fatalf("postimage: %v", after)
	}
	if len(base) != 2 {
		t.Fatal("base mutated")
	}
}

func TestAddNoFinalNewlineAndQuotedPath(t *testing.T) {
	patch := "diff --git \"a/a file.py\" \"b/a file.py\"\nnew file mode 100644\n--- /dev/null\n+++ \"b/a file.py\"\n@@ -0,0 +1 @@\n+x = 1\n\\ No newline at end of file\n"
	changes, err := Parse(strings.NewReader(patch))
	if err != nil {
		t.Fatal(err)
	}
	after, err := Apply(nil, changes)
	if err != nil {
		t.Fatal(err)
	}
	if string(after["a file.py"]) != "x = 1" {
		t.Fatalf("postimage: %v", after)
	}
}

func TestRejectInvalidInput(t *testing.T) {
	for _, patch := range []string{"not a diff", "diff --git a/../bad b/../bad\n--- a/../bad\n+++ b/../bad\n@@ -1 +1 @@\n-a\n+b\n", "diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1,4 +1 @@\n-a\n+b\n"} {
		if _, err := Parse(strings.NewReader(patch)); err == nil {
			t.Errorf("accepted %q", patch)
		}
	}
}

func TestRejectMissingBase(t *testing.T) {
	changes, err := Parse(strings.NewReader("diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1 +1 @@\n-old\n+new\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Apply(nil, changes); err == nil {
		t.Fatal("accepted missing base")
	}
}
