// Package git reads repository snapshots; it never changes the index or worktree.
package git

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
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

type blob struct {
	name, oid string
	symlink   bool
}

type Snapshot struct {
	Files   map[string][]byte
	Links   map[string]string
	Modules map[string]string
	Opaque  map[string]string
	Issues  []string
}

type Repository struct {
	Root  string
	depth int
}

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
	s := newSnapshot()
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
		if fields[0] == "160000" {
			r.readModule(ctx, &s, name, fields[2], false)
			continue
		}
		if fields[0] != "100644" && fields[0] != "100755" && fields[0] != "120000" {
			s.Issues = append(s.Issues, name+": unsupported Git entry mode")
			continue
		}
		blobs = append(blobs, blob{name, fields[2], fields[0] == "120000"})
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
			digest := sha256.New()
			if _, err = io.CopyN(digest, reader, int64(size)); err != nil {
				return s, err
			}
			if _, err = reader.ReadByte(); err != nil {
				return s, err
			}
			s.Opaque[b.name] = fmt.Sprintf("%d:sha256:%x", size, digest.Sum(nil))
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
		if b.symlink {
			s.Links[b.name] = string(data[:size])
		} else {
			s.Files[b.name] = data[:size]
		}
	}
	err = cmd.Wait()
	finished = true
	if err != nil {
		return s, fmt.Errorf("git cat-file: %w: %s", err, stderr.String())
	}
	s.checkLinks()
	return s, nil
}

func (r *Repository) Working(ctx context.Context) (Snapshot, []string, error) {
	s := newSnapshot()
	tracked, err := r.run(ctx, "ls-files", "-z", "--cached")
	if err != nil {
		return s, nil, err
	}
	untracked, err := r.run(ctx, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return s, nil, err
	}
	entries, err := r.run(ctx, "ls-files", "--stage", "-z")
	if err != nil {
		return s, nil, err
	}
	modules := map[string]string{}
	for _, line := range strings.Split(string(entries), "\x00") {
		if line == "" {
			continue
		}
		meta, name, ok := strings.Cut(line, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 || !diff.ValidPath(name) {
			return s, nil, fmt.Errorf("invalid index entry")
		}
		if fields[2] != "0" {
			return s, nil, fmt.Errorf("unmerged index entry: %s", name)
		}
		if fields[0] == "160000" {
			modules[name] = fields[1]
		}
	}
	names := map[string]bool{}
	var added []string
	for _, name := range strings.Split(string(tracked), "\x00") {
		if name != "" {
			names[name] = true
		}
	}
	for _, name := range strings.Split(string(untracked), "\x00") {
		// Without --directory, ls-files emits trailing slashes for untracked
		// embedded repositories, including linked worktrees. They belong to
		// another checkout, not to this repository's files or added changes.
		// Validate the directory spelling without weakening file-path checks.
		if directory, ok := strings.CutSuffix(name, "/"); ok {
			if !diff.ValidPath(directory) {
				return s, nil, fmt.Errorf("unsafe repository path %q", name)
			}
			continue
		}
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
		if oid, ok := modules[name]; ok {
			r.readModule(ctx, &s, name, oid, true)
			continue
		}
		full := filepath.Join(r.Root, filepath.FromSlash(name))
		info, err := os.Lstat(full)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return s, nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(full)
			if err != nil {
				return s, nil, err
			}
			s.Links[name] = target
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
			file, err := os.Open(full)
			if err != nil {
				return s, nil, err
			}
			digest := sha256.New()
			size, err := io.Copy(digest, contextReader{ctx: ctx, reader: file})
			closeErr := file.Close()
			if err != nil {
				return s, nil, err
			}
			if closeErr != nil {
				return s, nil, closeErr
			}
			s.Opaque[name] = fmt.Sprintf("%d:sha256:%x", size, digest.Sum(nil))
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
	if err := ctx.Err(); err != nil {
		return s, nil, err
	}
	s.checkLinks()
	return s, added, nil
}
