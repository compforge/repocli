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
// Internal targets are already hashed as files (or recursively captured modules).
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
			if current == module || strings.HasPrefix(current, module+"/") {
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
	// Bound nested recursion; each repository also uses the shared file/byte limits.
	if r.depth >= 8 {
		s.Issues = append(s.Issues, name+": submodule nesting exceeds 8 levels")
		return
	}
	dir := filepath.Join(r.Root, filepath.FromSlash(name))
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil || resolved != dir {
		s.Issues = append(s.Issues, name+": submodule checkout unavailable or symlinked")
		return
	}
	if _, err := os.Lstat(filepath.Join(dir, ".git")); err != nil {
		s.Issues = append(s.Issues, name+": submodule is not initialized")
		return
	}
	child, err := Open(ctx, dir)
	if err != nil || child.Root != dir {
		s.Issues = append(s.Issues, name+": submodule checkout unavailable")
		return
	}
	child.depth = r.depth + 1
	var captured Snapshot
	if working {
		oid, err = child.Resolve(ctx, "HEAD")
		if err == nil {
			captured, _, err = child.Working(ctx)
		}
	} else {
		captured, err = child.Base(ctx, oid)
	}
	if err != nil {
		s.Issues = append(s.Issues, name+": cannot capture submodule contents: "+err.Error())
		return
	}
	// HEAD alone misses unstaged, staged and untracked edits inside the submodule.
	s.Modules[name] = fmt.Sprintf("%s:%s", oid, captured.Digest())
	// Config adapters may consult these bytes on demand. Child code remains out
	// of the parent's source catalog, graph expansion and component discovery.
	for file, data := range captured.Files {
		if strings.HasSuffix(file, ".json") {
			s.Resources[path.Join(name, file)] = data
		}
	}
	for file, data := range captured.Resources {
		s.Resources[path.Join(name, file)] = data
	}
	for _, issue := range captured.Issues {
		s.Issues = append(s.Issues, name+"/"+issue)
	}
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
