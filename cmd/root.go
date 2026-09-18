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

// Version is supplied by the executable from its embedded VERSION file.
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
	return execute(ctx, newRootCommand(&options{}), args, stdin, stdout, stderr)
}

// Keep the execution boundary independent of subcommand handlers: new commands,
// Cobra help, and errors raised before RunE all use the same log lifecycle.
func execute(ctx context.Context, root *cobra.Command, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	// Cobra interprets nil args as os.Args; embedding callers own their input.
	if args == nil {
		args = []string{}
	}
	run := startCommandLog(stderr, args)
	defer run.close()
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(logStream{output: stdout, run: run, name: "stdout"})
	root.SetErr(logStream{output: stderr, run: run, name: "stderr"})
	command, err := root.ExecuteContextC(ctx)
	code := 0
	if err != nil {
		fmt.Fprintln(root.ErrOrStderr(), "repocli:", err)
		code = 2
		var failure executionError
		if errors.As(err, &failure) {
			code = 1
		}
	}
	name := root.CommandPath()
	if command != nil {
		name = command.CommandPath()
	}
	run.finish(name, code, err)
	return code
}

func newRootCommand(opts *options) *cobra.Command {
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
	root.AddCommand(newDiffCommand(opts), newVersionCommand(opts, root.Version))
	return root
}
