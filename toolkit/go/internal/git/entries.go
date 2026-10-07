package git

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

func newSnapshot() Snapshot {
	return Snapshot{Resources: map[string][]byte{}, Files: map[string][]byte{}, Links: map[string]string{}, Modules: map[string]string{}, Opaque: map[string]string{}}
}

// Link targets are data. Never follow them into files outside the captured input.
// Internal targets are already hashed as files.
func (s *Snapshot) checkLinks() {
	names := make([]string, 0, len(s.Links))
	for name := range s.Links {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		current := name
		seen := map[string]bool{}
		for {
			target, ok := s.Links[current]
			if !ok {
				break
			}
			if seen[current] {
				current = ""
				break
			}
			seen[current] = true
			if path.IsAbs(target) {
				current = ""
				break
			}
			current = path.Clean(path.Join(path.Dir(current), target))
			if current == ".." || strings.HasPrefix(current, "../") {
				current = ""
				break
			}
		}
		_, captured := s.Files[current]
		if _, ok := s.Opaque[current]; ok {
			captured = true
		}
		for module := range s.Modules {
			if current == module {
				captured = true
			}
		}
		if !captured && current != "" {
			for file := range s.Files {
				if current == "." || strings.HasPrefix(file, current+"/") {
					captured = true
					break
				}
			}
		}
		if current == "" || !captured {
			s.Issues = append(s.Issues, name+": symlink target is outside captured contents or cyclic/missing")
		}
	}
}

func (r *Repository) readModule(ctx context.Context, s *Snapshot, name, oid string, working bool) {
	if working {
		var err error
		oid, err = r.gitlinkOID(ctx, name, oid)
		if err != nil {
			s.Issues = append(s.Issues, name+": "+err.Error())
			return
		}
	}
	s.Modules[name] = oid
}

// Read only a checkout's pointer; absence leaves the parent index reference valid.
func (r *Repository) gitlinkOID(ctx context.Context, name, oid string) (string, error) {
	dir := filepath.Join(r.Root, filepath.FromSlash(name))
	if _, err := os.Lstat(filepath.Join(dir, ".git")); os.IsNotExist(err) {
		return oid, nil
	} else if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	if resolved != dir {
		return "", fmt.Errorf("symlinked gitlink checkout")
	}
	child, err := Open(ctx, dir)
	if err != nil {
		return "", err
	}
	if child.Root != dir {
		return "", fmt.Errorf("invalid gitlink checkout")
	}
	return child.Resolve(ctx, "HEAD")
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
