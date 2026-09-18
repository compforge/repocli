// Package git reads repository snapshots; it never changes the index or worktree.
package git

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/compforge/repocli/internal/diff"
)

const maxFiles = 10000
const maxFileBytes = 2 << 20
const maxSnapshotBytes = 128 << 20

type blob struct{ name, oid string }

type Snapshot struct {
	Files  map[string][]byte
	Issues []string
}

type Repository struct{ Root string }

func (r *Repository) Origin(ctx context.Context) (string, error) {
	out, err := r.run(ctx, "config", "--get", "remote.origin.url")
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return "", nil
	}
	return strings.TrimSpace(string(out)), err
}

func Open(ctx context.Context, dir string) (*Repository, error) {
	r := &Repository{Root: dir}
	out, err := r.run(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	r.Root = strings.TrimSpace(string(out))
	return r, nil
}

func (r *Repository) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-pager"}, args...)...)
	cmd.Dir = r.Root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func (r *Repository) Resolve(ctx context.Context, ref string) (string, error) {
	out, err := r.run(ctx, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	return strings.TrimSpace(string(out)), err
}

func (r *Repository) Patch(ctx context.Context, base string) ([]byte, error) {
	return r.run(ctx, "diff", "--no-ext-diff", "--no-textconv", "--binary", "--find-renames", base, "--")
}

func (r *Repository) Base(ctx context.Context, ref string) (Snapshot, error) {
	s := Snapshot{Files: map[string][]byte{}}
	out, err := r.run(ctx, "ls-tree", "-r", "-z", "--full-tree", ref)
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
			return s, fmt.Errorf("invalid git tree entry")
		}
		if fields[0] != "100644" && fields[0] != "100755" {
			s.Issues = append(s.Issues, name+": symlink or submodule is not analyzed")
			continue
		}
		blobs = append(blobs, blob{name, fields[2]})
	}
	return r.readBlobs(ctx, s, blobs)
}

func (r *Repository) readBlobs(ctx context.Context, s Snapshot, blobs []blob) (Snapshot, error) {
	if len(blobs) > maxFiles {
		return s, fmt.Errorf("repository exceeds %d files", maxFiles)
	}
	// One cat-file process avoids spawning Git once for every unchanged file.
	cmd := exec.CommandContext(ctx, "git", "cat-file", "--batch")
	cmd.Dir = r.Root
	var input strings.Builder
	for _, b := range blobs {
		fmt.Fprintln(&input, b.oid)
	}
	cmd.Stdin = strings.NewReader(input.String())
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return s, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return s, err
	}
	finished := false
	defer func() {
		if !finished {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	reader := bufio.NewReader(pipe)
	total := 0
	for _, b := range blobs {
		header, err := reader.ReadString('\n')
		if err != nil {
			return s, fmt.Errorf("read blob %s: %w", b.name, err)
		}
		parts := strings.Fields(header)
		if len(parts) != 3 || parts[1] != "blob" {
			return s, fmt.Errorf("invalid blob header for %s", b.name)
		}
		size, err := strconv.Atoi(parts[2])
		if err != nil || size < 0 {
			return s, fmt.Errorf("invalid blob size for %s", b.name)
		}
		if size > maxFileBytes {
			if _, err = io.CopyN(io.Discard, reader, int64(size)+1); err != nil {
				return s, err
			}
			s.Issues = append(s.Issues, b.name+": file exceeds 2 MiB")
			continue
		}
		total += size
		if total > maxSnapshotBytes {
			return s, fmt.Errorf("snapshot exceeds 128 MiB")
		}
		data := make([]byte, size+1)
		if _, err = io.ReadFull(reader, data); err != nil {
			return s, err
		}
		s.Files[b.name] = data[:size]
	}
	err = cmd.Wait()
	finished = true
	if err != nil {
		return s, fmt.Errorf("git cat-file: %w: %s", err, stderr.String())
	}
	return s, nil
}

func (r *Repository) Working(ctx context.Context) (Snapshot, []string, error) {
	s := Snapshot{Files: map[string][]byte{}}
	tracked, err := r.run(ctx, "ls-files", "-z", "--cached")
	if err != nil {
		return s, nil, err
	}
	untracked, err := r.run(ctx, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return s, nil, err
	}
	names := map[string]bool{}
	var added []string
	for _, name := range strings.Split(string(tracked), "\x00") {
		if name != "" {
			names[name] = true
		}
	}
	for _, name := range strings.Split(string(untracked), "\x00") {
		if name != "" {
			names[name] = true
			added = append(added, name)
		}
	}
	if len(names) > maxFiles {
		return s, nil, fmt.Errorf("repository exceeds %d files", maxFiles)
	}
	paths := make([]string, 0, len(names))
	for name := range names {
		paths = append(paths, name)
	}
	sort.Strings(paths)
	total := 0
	for _, name := range paths {
		if err := ctx.Err(); err != nil {
			return s, nil, err
		}
		if !diff.ValidPath(name) {
			return s, nil, fmt.Errorf("unsafe repository path %q", name)
		}
		full := filepath.Join(r.Root, filepath.FromSlash(name))
		// A dangling symlink is an unsupported entry, not a deleted file.
		info, err := os.Lstat(full)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return s, nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			s.Issues = append(s.Issues, name+": symlink is not analyzed")
			continue
		}
		resolved, err := filepath.EvalSymlinks(full)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return s, nil, err
		}
		if resolved != full {
			s.Issues = append(s.Issues, name+": symlink is not analyzed")
			continue
		}
		if !info.Mode().IsRegular() {
			s.Issues = append(s.Issues, name+": non-regular file is not analyzed")
			continue
		}
		if info.Size() > maxFileBytes {
			s.Issues = append(s.Issues, name+": file exceeds 2 MiB")
			continue
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return s, nil, err
		}
		total += len(data)
		if total > maxSnapshotBytes {
			return s, nil, fmt.Errorf("snapshot exceeds 128 MiB")
		}
		s.Files[name] = data
	}
	return s, added, nil
}
