// Package diff parses Git patches without interpreting their source language.
package diff

import (
	"bytes"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

// Range uses one-based lines. A zero count denotes an insertion/deletion anchor.
type Range struct {
	Start int `json:"start"`
	Count int `json:"count"`
}

type Hunk struct {
	Old Range `json:"old"`
	New Range `json:"new"`
}

type Change struct {
	Path    string `json:"path"`
	OldPath string `json:"oldPath,omitempty"`
	Status  string `json:"status"`
	Binary  bool   `json:"binary,omitempty"`
	Hunks   []Hunk `json:"hunks"`
	patch   *gitdiff.File
}

func Parse(r io.Reader) ([]Change, error) {
	files, preamble, err := gitdiff.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("parse diff: %w", err)
	}
	if len(files) == 0 && strings.TrimSpace(preamble) != "" {
		return nil, fmt.Errorf("input contains no Git patch")
	}
	changes := make([]Change, 0, len(files))
	seen := make(map[string]bool)
	for _, f := range files {
		for _, name := range []string{f.OldName, f.NewName} {
			if name != "" && !ValidPath(name) {
				return nil, fmt.Errorf("unsafe diff path %q", name)
			}
		}
		c := Change{Path: f.NewName, Status: "modified", Binary: f.IsBinary, Hunks: []Hunk{}, patch: f}
		switch {
		case f.IsNew:
			c.Status = "added"
		case f.IsDelete:
			c.Status, c.Path = "deleted", f.OldName
		case f.IsRename:
			c.Status, c.OldPath = "renamed", f.OldName
		case f.IsCopy:
			c.Status, c.OldPath = "copied", f.OldName
		}
		if c.Path == "" || seen[c.Path] {
			return nil, fmt.Errorf("empty or repeated diff path %q", c.Path)
		}
		seen[c.Path] = true
		for _, h := range f.TextFragments {
			// Context is not a change: keep only contiguous +/- runs, even in a
			// normal three-context-line patch, so neighbouring symbols stay out.
			oldLine, newLine := int(h.OldPosition), int(h.NewPosition)
			var run *Hunk
			flush := func() {
				if run != nil {
					c.Hunks = append(c.Hunks, *run)
					run = nil
				}
			}
			for _, line := range h.Lines {
				if line.Op == gitdiff.OpContext {
					flush()
					oldLine++
					newLine++
					continue
				}
				if run == nil {
					run = &Hunk{Old: Range{Start: oldLine}, New: Range{Start: newLine}}
				}
				if line.Op == gitdiff.OpDelete {
					run.Old.Count++
					oldLine++
				}
				if line.Op == gitdiff.OpAdd {
					run.New.Count++
					newLine++
				}
			}
			flush()
		}
		changes = append(changes, c)
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, nil
}

func ValidPath(p string) bool {
	return p != "." && p != "" && !strings.ContainsAny(p, "\x00\\") &&
		!strings.HasPrefix(p, "/") && p != ".." && !strings.HasPrefix(p, "../") && path.Clean(p) == p
}

// Apply reconstructs a patch's postimage in memory. No Git index or worktree is written.
func Apply(base map[string][]byte, changes []Change) (map[string][]byte, error) {
	result := make(map[string][]byte, len(base))
	for name, data := range base {
		result[name] = data
	}
	for _, c := range changes {
		f := c.patch
		old, exists := base[f.OldName]
		if !f.IsNew && !exists {
			return nil, fmt.Errorf("patch base missing %q; specify the matching --base", f.OldName)
		}
		if f.IsNew {
			if _, exists := base[c.Path]; exists {
				return nil, fmt.Errorf("patch adds existing file %q", c.Path)
			}
		}
		if f.IsBinary && f.BinaryFragment == nil {
			return nil, fmt.Errorf("%s: binary patch has no content; generate with git diff --binary", c.Path)
		}
		var out bytes.Buffer
		if err := gitdiff.Apply(&out, bytes.NewReader(old), f); err != nil {
			return nil, fmt.Errorf("apply %s to base: %w", c.Path, err)
		}
		if f.IsDelete || f.IsRename {
			delete(result, f.OldName)
		}
		if !f.IsDelete {
			result[c.Path] = out.Bytes()
		}
	}
	return result, nil
}

func Added(name string, content []byte) Change {
	lines := bytes.Count(content, []byte{'\n'})
	if len(content) > 0 && content[len(content)-1] != '\n' {
		lines++
	}
	return Change{Path: name, Status: "added", Binary: bytes.IndexByte(content, 0) >= 0,
		Hunks: []Hunk{{Old: Range{}, New: Range{Start: 1, Count: lines}}}}
}
