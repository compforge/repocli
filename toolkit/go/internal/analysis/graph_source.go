package analysis

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/compforge/repocli/toolkit/go/internal/git"
)

// GraphSource is the read-only material view behind one graph publication.
// Callers supplying a captured view keep its identity and bytes together.
type GraphSource interface {
	SnapshotID() string
	ReadDirectory(context.Context, string) (SourceDirectory, error)
	ReadSource(context.Context, string) (string, error)
}

type retainedGraphSource struct {
	*SourceTree
	id       string
	contents git.Snapshot
}

func newRetainedGraphSource(id string, contents git.Snapshot) (*retainedGraphSource, error) {
	files := make([]SourceFile, 0, len(contents.Files)+len(contents.Opaque))
	for name, data := range contents.Files {
		files = append(files, SourceFile{Path: name, Size: int64(len(data))})
	}
	for name := range contents.Opaque {
		files = append(files, SourceFile{Path: name, Size: -1})
	}
	tree, err := NewSourceTree(files)
	if err != nil {
		return nil, err
	}
	return &retainedGraphSource{SourceTree: tree, id: id, contents: contents}, nil
}

func (s retainedGraphSource) SnapshotID() string { return s.id }
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
		return newRetainedGraphSource(id, captured.Contents)
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

// ReadDirectory lists only direct children. The caller decides whether a child
// directory may be entered; source adapters hold no graph admission policy.
func (s directoryGraphSource) ReadDirectory(ctx context.Context, name string) (SourceDirectory, error) {
	if err := ctx.Err(); err != nil {
		return SourceDirectory{}, err
	}
	if !fs.ValidPath(name) {
		return SourceDirectory{}, fmt.Errorf("invalid source directory %q", name)
	}
	entries, err := os.ReadDir(filepath.Join(s.root, filepath.FromSlash(name)))
	if err != nil {
		return SourceDirectory{}, fmt.Errorf("read source directory %s: %w", name, err)
	}
	var out SourceDirectory
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return SourceDirectory{}, err
		}
		if entry.Name() == ".git" || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		child := filepath.ToSlash(filepath.Join(name, entry.Name()))
		if entry.IsDir() {
			out.Directories = append(out.Directories, child)
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return SourceDirectory{}, fmt.Errorf("stat source %s: %w", child, err)
		}
		if info.Mode().IsRegular() {
			out.Documents = append(out.Documents, SourceFile{Path: child, Size: info.Size()})
		}
	}
	return out, nil
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
