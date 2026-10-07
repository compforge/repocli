package repocli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// GitResult retains failed and uncertain outcomes. Cancellation after starting a
// write can leave effects behind; inspect the repository before retrying.
type GitResult struct {
	ExitCode  int
	Stdout    string
	Stderr    string
	Uncertain bool
	Err       error
}

// WorktreeEntry is a registered Git worktree. The first entry can be a bare or
// separate metadata directory rather than a usable checkout.
type WorktreeEntry struct {
	Path   string
	SHA    string
	Branch string // Empty for detached HEAD or bare repositories.
}

// CheckoutInfo describes Git topology, without caller-specific state placement.
type CheckoutInfo struct {
	Root      string
	GitDir    string
	CommonDir string
	MainRoot  string // Empty when shared metadata cannot identify the main checkout.
	Linked    bool
}

func worktreeGit(ctx context.Context, repo string, timeout time.Duration, args ...string) GitResult {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	cmd.WaitDelay = time.Second
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err := cmd.Run()
	code := -1
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	// A failed start has no side effect. Once started, cancellation is indeterminate.
	uncertain := cmd.Process != nil && ctx.Err() != nil
	if err != nil {
		err = fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return GitResult{ExitCode: code, Stdout: out.String(), Stderr: stderr.String(), Uncertain: uncertain, Err: err}
}

// ListWorktrees reads NUL-delimited Git records; failure never means an empty inventory.
func ListWorktrees(ctx context.Context, repo string) ([]WorktreeEntry, error) {
	result := worktreeGit(ctx, repo, 5*time.Second, "worktree", "list", "--porcelain", "-z")
	if result.Err != nil {
		return nil, result.Err
	}
	var entries []WorktreeEntry
	for _, block := range strings.Split(result.Stdout, "\x00\x00") {
		var entry WorktreeEntry
		for _, field := range strings.Split(block, "\x00") {
			key, value, _ := strings.Cut(field, " ")
			switch key {
			case "worktree":
				entry.Path = value
			case "HEAD":
				entry.SHA = value
			case "branch":
				entry.Branch = strings.TrimPrefix(value, "refs/heads/")
			}
		}
		if entry.Path != "" {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

// Checkout observes the current checkout and shared metadata. Separate Git dirs
// need not retain a backlink to their original checkout, so MainRoot may be empty.
func Checkout(ctx context.Context, repo string) (CheckoutInfo, error) {
	query := func(path, flag string) (string, error) {
		result := worktreeGit(ctx, path, 5*time.Second, "rev-parse", "--path-format=absolute", flag)
		if result.Err != nil {
			return "", result.Err
		}
		return filepath.EvalSymlinks(strings.TrimSuffix(result.Stdout, "\n"))
	}
	var info CheckoutInfo
	var err error
	if info.Root, err = query(repo, "--show-toplevel"); err != nil {
		return info, err
	}
	if info.GitDir, err = query(repo, "--git-dir"); err != nil {
		return info, err
	}
	if info.CommonDir, err = query(repo, "--git-common-dir"); err != nil {
		return info, err
	}
	info.Linked = info.GitDir != info.CommonDir
	entries, err := ListWorktrees(ctx, repo)
	if err != nil {
		return info, err
	}
	if len(entries) > 0 {
		if candidate, err := query(entries[0].Path, "--show-toplevel"); err == nil {
			// A metadata directory may masquerade as its own work tree, but has no .git entry.
			if _, err := os.Stat(filepath.Join(candidate, ".git")); err == nil {
				if common, err := query(candidate, "--git-common-dir"); err == nil && common == info.CommonDir {
					info.MainRoot = candidate
				}
			}
		}
	}
	return info, nil
}

// AddWorktreeOptions chooses a new branch or a detached checkout. Both empty
// leaves Git's normal branch selection intact. Naming and ownership are caller policy.
type AddWorktreeOptions struct {
	Branch string
	Detach bool
}

// AddWorktree creates a checkout at an explicit path. It never replaces an existing branch.
func AddWorktree(ctx context.Context, repo, path, ref string, options AddWorktreeOptions) GitResult {
	if options.Branch != "" && options.Detach {
		return GitResult{ExitCode: -1, Err: errors.New("branch and detach are mutually exclusive")}
	}
	args := []string{"worktree", "add"}
	if options.Branch != "" {
		args = append(args, "-b", options.Branch)
	}
	if options.Detach {
		args = append(args, "--detach")
	}
	return worktreeGit(ctx, repo, 30*time.Second, append(args, "--", path, ref)...)
}

// RemoveWorktreeOptions requires explicit force to remove dirty or locked checkouts.
type RemoveWorktreeOptions struct{ Force bool }

// RemoveWorktree defaults to Git's protection of dirty and locked checkouts.
// It retains branch refs and does not choose which worktrees are eligible for removal.
func RemoveWorktree(ctx context.Context, repo, path string, options RemoveWorktreeOptions) GitResult {
	args := []string{"worktree", "remove"}
	if options.Force {
		args = append(args, "--force", "--force")
	}
	return worktreeGit(ctx, repo, 30*time.Second, append(args, "--", path)...)
}
