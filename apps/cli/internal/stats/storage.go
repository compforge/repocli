package stats

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func (r *Report) regularDirectory(name string) bool {
	path := filepath.Join(r.Directory, name)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if err == nil && !info.IsDir() {
		err = fmt.Errorf("not a regular directory (symlinks are not followed)")
	}
	if err != nil {
		r.Diagnostics = append(r.Diagnostics, fmt.Sprintf("%s: %v", path, err))
		return false
	}
	return true
}

func (r *Report) readStorage(ctx context.Context) {
	groups := map[string]*Storage{}
	err := filepath.WalkDir(r.Directory, func(path string, entry fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if path == r.Directory && errors.Is(err, os.ErrNotExist) {
				return nil
			}
			r.Diagnostics = append(r.Diagnostics, fmt.Sprintf("storage %s: %v", path, err))
			return nil
		}
		if path == r.Directory {
			return nil
		}
		rel, err := filepath.Rel(r.Directory, path)
		if err != nil {
			return err
		}
		name, _, nested := strings.Cut(rel, string(filepath.Separator))
		if !nested && !entry.IsDir() {
			name = "."
		}
		group := groups[name]
		if group == nil {
			group = &Storage{Name: name}
			groups[name] = group
		}
		if entry.Type()&os.ModeSymlink != 0 {
			group.Symlinks++
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			r.Diagnostics = append(r.Diagnostics, fmt.Sprintf("storage %s: %v", path, err))
			return nil
		}
		group.Files++
		group.Bytes += info.Size()
		includeTime(&group.OldestModifiedAt, &group.NewestModifiedAt, info.ModTime())
		return nil
	})
	if err != nil {
		r.Diagnostics = append(r.Diagnostics, "storage: "+err.Error())
	}
	for _, name := range sortedKeys(groups) {
		r.Storage = append(r.Storage, *groups[name])
	}
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
