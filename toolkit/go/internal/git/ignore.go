package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

// IgnoredFiles applies the captured .gitignore files, including to tracked paths.
// +spec=`Ignore rules belong to the selected snapshot, not the live checkout`
// +why=`Git owns wildcard, nested rule and negation semantics`
func IgnoredFiles(ctx context.Context, files map[string][]byte) (map[string]bool, error) {
	ignored := make(map[string]bool)
	var rules []string
	for name := range files {
		if path.Base(name) == ".gitignore" {
			rules = append(rules, name)
		}
	}
	if len(rules) == 0 {
		return ignored, nil
	}
	root, err := os.MkdirTemp("", "repocli-ignore-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(root)
	for _, name := range rules {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(full, files[name], 0600); err != nil {
			return nil, err
		}
	}
	// An isolated Git directory keeps checkout-local excludes and index entries
	// from changing the captured repository rules.
	repo := &Repository{Root: root}
	if _, err := repo.run(ctx, "init", "--quiet", "--template="); err != nil {
		return nil, err
	}
	var input, stderr bytes.Buffer
	for name := range files {
		input.WriteString(name)
		input.WriteByte(0)
	}
	cmd := exec.CommandContext(ctx, "git", "-c", "core.excludesFile="+os.DevNull,
		"check-ignore", "--no-index", "--stdin", "-z")
	cmd.Dir, cmd.Stdin, cmd.Stderr = root, &input, &stderr
	out, err := cmd.Output()
	var exit *exec.ExitError
	if err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 1) {
		return nil, fmt.Errorf("git check-ignore: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	for _, name := range strings.Split(string(out), "\x00") {
		if name != "" {
			ignored[name] = true
		}
	}
	return ignored, nil
}
