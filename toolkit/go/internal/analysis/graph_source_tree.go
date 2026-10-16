package analysis

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"sort"
)

// SourceDirectory lists immediate children using repository-relative paths.
// Documents retain unavailable bodies with Size=-1; listing never loads bodies.
type SourceDirectory struct {
	Directories []string
	Documents   []SourceFile
}

// SourceTree indexes an already captured file catalog for directory enumeration.
// It never opens Git or the filesystem and owns its detached directory entries.
type SourceTree struct{ directories map[string]SourceDirectory }

func NewSourceTree(files []SourceFile) (*SourceTree, error) {
	tree := &SourceTree{directories: map[string]SourceDirectory{".": {}}}
	var directory func(string)
	directory = func(name string) {
		if _, ok := tree.directories[name]; ok {
			return
		}
		tree.directories[name] = SourceDirectory{}
		parent := path.Dir(name)
		directory(parent)
		d := tree.directories[parent]
		d.Directories = append(d.Directories, name)
		tree.directories[parent] = d
	}
	for _, file := range files {
		if !fs.ValidPath(file.Path) || file.Path == "." {
			return nil, fmt.Errorf("invalid graph source path %q", file.Path)
		}
		parent := path.Dir(file.Path)
		directory(parent)
		d := tree.directories[parent]
		d.Documents = append(d.Documents, file)
		tree.directories[parent] = d
	}
	for name, d := range tree.directories {
		slices.Sort(d.Directories)
		sort.Slice(d.Documents, func(i, j int) bool { return d.Documents[i].Path < d.Documents[j].Path })
		tree.directories[name] = d
	}
	return tree, nil
}

func (s *SourceTree) ReadDirectory(ctx context.Context, name string) (SourceDirectory, error) {
	if err := ctx.Err(); err != nil {
		return SourceDirectory{}, err
	}
	d, ok := s.directories[name]
	if !ok {
		return SourceDirectory{}, fmt.Errorf("source directory %s: %w", name, fs.ErrNotExist)
	}
	return SourceDirectory{Directories: slices.Clone(d.Directories), Documents: slices.Clone(d.Documents)}, nil
}
