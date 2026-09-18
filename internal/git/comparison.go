package git

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	"github.com/compforge/repocli/internal/diff"
)

// ComparisonPatch uses resolved object IDs; user refs never become Git options.
func (r *Repository) ComparisonPatch(ctx context.Context, base, head string, staged bool) ([]byte, error) {
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--binary", "--find-renames"}
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, base)
	if head != "" {
		args = append(args, head)
	}
	return r.run(ctx, append(args, "--")...)
}

// Staged reads the index without writing a tree or changing its stat cache.
func (r *Repository) Staged(ctx context.Context) (Snapshot, error) {
	s := Snapshot{Files: map[string][]byte{}}
	out, err := r.run(ctx, "ls-files", "--stage", "-z")
	if err != nil {
		return s, err
	}
	var blobs []blob
	for _, line := range strings.Split(string(out), "\x00") {
		if line == "" {
			continue
		}
		meta, name, ok := strings.Cut(line, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 || !diff.ValidPath(name) {
			return s, fmt.Errorf("invalid index entry")
		}
		if fields[2] != "0" {
			return s, fmt.Errorf("unmerged index entry: %s", name)
		}
		if fields[0] != "100644" && fields[0] != "100755" {
			s.Issues = append(s.Issues, name+": symlink or submodule is not analyzed")
			continue
		}
		blobs = append(blobs, blob{name, fields[1]})
	}
	return r.readBlobs(ctx, s, blobs)
}

// Digest identifies observed contents, not an atomic filesystem snapshot.
func (s Snapshot) Digest() string {
	h := sha256.New()
	names := make([]string, 0, len(s.Files))
	for name := range s.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data := s.Files[name]
		fmt.Fprintf(h, "%d:%s%d:", len(name), name, len(data))
		h.Write(data)
	}
	issues := append([]string{}, s.Issues...)
	sort.Strings(issues)
	for _, issue := range issues {
		fmt.Fprintf(h, "issue:%d:%s", len(issue), issue)
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil))
}
