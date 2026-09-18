// Package cmd defines repocli's Cobra commands and command-line output.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"
)

// Version is set by release builds through -ldflags.
var Version = "dev"

type options struct {
	repository string
	json       bool
	timeout    time.Duration
}

// executionError distinguishes failed work from Cobra argument/usage errors.
// Keep the CLI contract: success=0, execution failure=1, invalid usage=2.
type executionError struct{ error }

// Execute runs a fresh command tree so flags and streams never leak between calls.
func Execute(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	root := newRootCommand()
	// Cobra interprets nil args as os.Args; embedding callers own their input.
	if args == nil {
		args = []string{}
	}
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(stderr, "repocli:", err)
		var failure executionError
		if errors.As(err, &failure) {
			return 1
		}
		return 2
	}
	return 0
}

func newRootCommand() *cobra.Command {
	opts := &options{}
	root := &cobra.Command{
		Use:           "repocli",
		Version:       Version,
		Short:         "Tools for Git repositories",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	flags := root.PersistentFlags()
	flags.StringVar(&opts.repository, "repo", ".", "repository directory")
	flags.BoolVar(&opts.json, "json", false, "write structured JSON to stdout")
	flags.DurationVar(&opts.timeout, "timeout", 2*time.Minute, "analysis deadline")
	_ = root.MarkPersistentFlagDirname("repo")
	root.AddCommand(newDiffCommand(opts))
	return root
}
