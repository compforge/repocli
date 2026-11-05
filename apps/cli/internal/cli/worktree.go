package cli

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

// Python owns Forge writes, observation and dependency maintenance. This explicit
// operation adapter uses its packaged entrypoint; analysis remains native Go.
func newWorktreeCommand(opts *options) *cobra.Command {
	root := &cobra.Command{Use: "worktree", Short: "Maintain linked worktrees"}
	var dryRun bool
	var count, prepared int
	var provider, apiHost, stateDir string
	var targets []string
	clean := &cobra.Command{
		Use: "clean", Short: "Remove old worktrees and release old dependency environments",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.timeout <= 0 {
				return errors.New("--timeout must be positive")
			}
			executable, err := exec.LookPath("repokit-worktree")
			if err != nil {
				return executionError{fmt.Errorf("worktree clean requires the Python RepoKit operation runtime: uv tool install compforge-repokit==0.19.0: %w", err)}
			}
			args := []string{"--repo", opts.repository}
			if dryRun {
				args = append(args, "--dry-run")
			}
			if opts.json {
				args = append(args, "--json")
			}
			for _, flag := range []struct {
				name  string
				value int
			}{{"max-count", count}, {"max-prepared", prepared}} {
				if cmd.Flags().Changed(flag.name) {
					args = append(args, "--"+flag.name, strconv.Itoa(flag.value))
				}
			}
			for _, flag := range []struct{ name, value string }{{"provider", provider}, {"api-host", apiHost}, {"state-dir", stateDir}} {
				if flag.value != "" {
					args = append(args, "--"+flag.name, flag.value)
				}
			}
			for _, target := range targets {
				args = append(args, "--target-repository", target)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), opts.timeout)
			defer cancel()
			process := exec.CommandContext(ctx, executable, args...)
			process.WaitDelay = time.Second
			process.Stdin, process.Stdout, process.Stderr = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
			if err := process.Run(); err != nil {
				return executionError{fmt.Errorf("worktree clean: %w", err)}
			}
			return nil
		},
	}
	clean.Flags().BoolVar(&dryRun, "dry-run", false, "show decisions without deleting resources")
	clean.Flags().IntVar(&count, "max-count", 10, "maximum linked worktrees (repository config by default)")
	clean.Flags().IntVar(&prepared, "max-prepared", 3, "maximum prepared worktrees (repository config by default)")
	clean.Flags().StringVar(&provider, "provider", "", "Forge provider: github or gitlab")
	clean.Flags().StringVar(&apiHost, "api-host", "", "Forge API host override")
	clean.Flags().StringVar(&stateDir, "state-dir", "", "RepoKit observation state directory")
	clean.Flags().StringArrayVar(&targets, "target-repository", nil, "PR target repository path; repeat for forks")
	root.AddCommand(clean)
	return root
}
