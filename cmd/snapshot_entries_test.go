package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotInternalLinkIdentity(t *testing.T) {
	dir := fixture(t)
	put(t, dir, "AGENTS.md", "instructions")
	if err := os.Symlink("AGENTS.md", filepath.Join(dir, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, dir, "add", ".")
	gitCommand(t, dir, "commit", "-qm", "link")
	initial := snapshotJSON(t, dir)
	if !initial.Complete {
		t.Fatal(initial)
	}
	for _, flags := range [][]string{{"--staged"}, {"--head", "HEAD"}} {
		report := snapshotJSON(t, dir, flags...)
		if !report.Complete || report.Snapshot != initial.Snapshot {
			t.Fatal(report)
		}
	}
	put(t, dir, "AGENTS.md", "changed instructions")
	if snapshotJSON(t, dir).Snapshot == initial.Snapshot {
		t.Fatal("link target change not captured")
	}
	put(t, dir, "AGENTS.md", "instructions")
	if err := os.Remove(filepath.Join(dir, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("./AGENTS.md", filepath.Join(dir, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	if snapshotJSON(t, dir).Snapshot == initial.Snapshot {
		t.Fatal("link text change not captured")
	}
}

func TestSnapshotUnsafeLinksRemainIncomplete(t *testing.T) {
	for _, kind := range []string{"external", "cycle", "ignored"} {
		t.Run(kind, func(t *testing.T) {
			dir := fixture(t)
			target := "linked"
			if kind == "external" {
				target = filepath.Join(t.TempDir(), "outside")
				if err := os.WriteFile(target, []byte("external"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "ignored" {
				target = "ignored.ts"
				put(t, dir, target, "ignored")
			}
			if err := os.Symlink(target, filepath.Join(dir, "linked")); err != nil {
				t.Fatal(err)
			}
			report := snapshotJSON(t, dir)
			if report.Complete || len(report.Diagnostics) == 0 {
				t.Fatal(report)
			}
		})
	}
}

func submoduleFixture(t *testing.T) (string, string) {
	t.Helper()
	origin := fixture(t)
	parent := fixture(t)
	gitCommand(t, parent, "-c", "protocol.file.allow=always", "submodule", "add", origin, "child")
	gitCommand(t, parent, "commit", "-qam", "submodule")
	return parent, filepath.Join(parent, "child")
}

func TestSubmoduleSnapshotTracksWorkingContents(t *testing.T) {
	parent, child := submoduleFixture(t)
	initial := snapshotJSON(t, parent)
	if !initial.Complete {
		t.Fatal(initial)
	}
	for _, flags := range [][]string{{"--staged"}, {"--head", "HEAD"}} {
		report := snapshotJSON(t, parent, flags...)
		if !report.Complete || report.Snapshot != initial.Snapshot {
			t.Fatal(report, initial)
		}
	}
	// A dirty child with unchanged HEAD must change the parent's identity.
	put(t, child, "source file.ts", "changed child bytes")
	dirty := snapshotJSON(t, parent)
	if !dirty.Complete || dirty.Snapshot == initial.Snapshot {
		t.Fatal(dirty)
	}
	gitCommand(t, child, "add", "source file.ts")
	if got := snapshotJSON(t, parent); got.Snapshot != dirty.Snapshot {
		t.Fatal("staging alone changed content identity")
	}
	if snapshotJSON(t, parent, "--staged").Snapshot != initial.Snapshot {
		t.Fatal("parent index read dirty submodule bytes")
	}
	gitCommand(t, child, "reset", "--hard", "HEAD")
	put(t, child, "new.txt", "untracked child input")
	if snapshotJSON(t, parent).Snapshot == initial.Snapshot {
		t.Fatal("untracked child input missed")
	}
	if err := os.Remove(filepath.Join(child, "new.txt")); err != nil {
		t.Fatal(err)
	}
	if snapshotJSON(t, parent).Snapshot != initial.Snapshot {
		t.Fatal("restored child differs")
	}
	put(t, child, "source file.ts", "new commit bytes")
	gitCommand(t, child, "add", ".")
	gitCommand(t, child, "commit", "-qm", "advance")
	if snapshotJSON(t, parent).Snapshot == initial.Snapshot {
		t.Fatal("child HEAD advance missed")
	}
	gitCommand(t, parent, "add", "child")
	if got := snapshotJSON(t, parent, "--staged"); got.Snapshot != snapshotJSON(t, parent).Snapshot {
		t.Fatal(got)
	}
}

func TestMissingSubmoduleFailsClosed(t *testing.T) {
	parent, _ := submoduleFixture(t)
	gitCommand(t, parent, "submodule", "deinit", "-f", "child")
	for _, flags := range [][]string{nil, {"--staged"}, {"--head", "HEAD"}} {
		report := snapshotJSON(t, parent, flags...)
		if report.Complete || len(report.Diagnostics) == 0 {
			t.Fatal(report)
		}
	}
}

func TestBunBuiltinsAreNotUnresolvedDependencies(t *testing.T) {
	dir := fixture(t)
	put(t, dir, "source file.ts", "export function a() { return 7; }\nexport function b() { return 2; }\n")
	put(t, dir, "tests/a.test.ts", "import {test} from 'bun:test';\nimport {a} from '../source file';\nimport {Database} from 'bun:sqlite';\nimport {dlopen} from 'bun:ffi';\nimport {gc} from 'bun:jsc';\nimport {serve} from 'bun';\n")
	report := runJSON(t, []string{"diff", "--repo", dir, "--impact", "file", "--test-dir", "tests", "--json"}, "")
	if !report.Complete || report.Scope != "focused" {
		t.Fatal(report)
	}
	// Keep the unknown importer unchanged: a test's own diff already proves
	// its membership, independently of any unknown import.
	put(t, dir, "tests/unknown.test.ts", "import {x} from 'bun:unknown';\n")
	gitCommand(t, dir, "add", "tests/unknown.test.ts")
	gitCommand(t, dir, "commit", "-qm", "unknown import candidate")
	report = runJSON(t, []string{"diff", "--repo", dir, "--impact", "file", "--test-dir", "tests", "--json"}, "")
	if report.Complete {
		t.Fatal("unknown builtin silently accepted")
	}
	found := false
	for _, d := range report.Diagnostics {
		if strings.Contains(d.Message, "bun:unknown") {
			found = true
		}
	}
	if !found {
		t.Fatal(report)
	}
}

func TestSnapshotStreamsLargeFiles(t *testing.T) {
	dir := fixture(t)
	data := strings.Repeat("x", (2<<20)+1)
	put(t, dir, "asset.bin", data)
	gitCommand(t, dir, "add", "asset.bin")
	gitCommand(t, dir, "commit", "-qm", "large asset")
	initial := snapshotJSON(t, dir)
	if !initial.Complete {
		t.Fatal(initial)
	}
	for _, flags := range [][]string{{"--staged"}, {"--head", "HEAD"}} {
		got := snapshotJSON(t, dir, flags...)
		if !got.Complete || got.Snapshot != initial.Snapshot {
			t.Fatal(got)
		}
	}
	put(t, dir, "asset.bin", "y"+data[1:])
	changed := snapshotJSON(t, dir)
	if !changed.Complete || changed.Snapshot == initial.Snapshot {
		t.Fatal(changed)
	}
}
