package analysis

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/compforge/repocli/toolkit/go/internal/git"
)

// GraphSource is the read-only material view behind one graph publication.
// Callers supplying a captured view keep its identity and bytes together.
type GraphSource interface {
	SnapshotID() string
	SourceFiles(context.Context) ([]SourceFile, error)
	ReadSource(context.Context, string) (string, error)
}

type retainedGraphSource struct {
	id       string
	contents git.Snapshot
}

func (s retainedGraphSource) SnapshotID() string { return s.id }
func (s retainedGraphSource) SourceFiles(ctx context.Context) ([]SourceFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]SourceFile, 0, len(s.contents.Files)+len(s.contents.Opaque))
	for name, data := range s.contents.Files {
		out = append(out, SourceFile{Path: name, Size: int64(len(data))})
	}
	for name := range s.contents.Opaque {
		out = append(out, SourceFile{Path: name, Size: -1})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}
func (s retainedGraphSource) ReadSource(ctx context.Context, name string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !fs.ValidPath(name) {
		return "", fmt.Errorf("invalid snapshot path %q", name)
	}
	if data, ok := s.contents.Files[name]; ok {
		return string(data), nil
	}
	if _, ok := s.contents.Opaque[name]; ok {
		return "", fmt.Errorf("%s: source exceeds capture limits", name)
	}
	return "", fmt.Errorf("%s: %w", name, fs.ErrNotExist)
}

// OpenGraphSource selects a pinned Git input or a live working directory.
// A working directory also supports unpacked source without .git; it is observed
// when enumerated and read, not an atomic snapshot. Retained inputs should instead
// implement GraphSource directly and must never be reopened here.
func OpenGraphSource(ctx context.Context, req InputRequest) (GraphSource, error) {
	if req.Head != "" || req.Staged {
		captured, err := Capture(ctx, req)
		if err != nil {
			return nil, err
		}
		id := captured.Report.Snapshot
		if captured.Report.Head != "" {
			id = captured.Report.Head
		}
		return retainedGraphSource{id: id, contents: captured.Contents}, nil
	}
	root, err := filepath.Abs(req.Repository)
	if err != nil {
		return nil, err
	}
	return directoryGraphSource{root: root}, nil
}

// Git administrative directories are outside the material catalog; graph admission
// owns all other directory and document exclusions.
type directoryGraphSource struct{ root string }

func (s directoryGraphSource) SnapshotID() string { return "working-directory" }
func (s directoryGraphSource) SourceFiles(ctx context.Context) ([]SourceFile, error) {
	var out []SourceFile
	err := filepath.WalkDir(s.root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(s.root, name)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			out = append(out, SourceFile{Path: rel, Size: info.Size()})
		}
		return nil
	})
	return out, err
}
func (s directoryGraphSource) ReadSource(ctx context.Context, name string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !fs.ValidPath(name) {
		return "", fmt.Errorf("invalid source path %q", name)
	}
	data, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(name)))
	return string(data), err
}
