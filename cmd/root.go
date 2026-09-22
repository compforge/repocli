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
// actual command handlers and errors raised before RunE share a log lifecycle.
func execute(ctx context.Context, root *cobra.Command, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	// Cobra interprets nil args as os.Args; embedding callers own their input.
	if args == nil {
		args = []string{}
	}
	run := &commandLog{}
	start := func() {
		if run.logger == nil {
			*run = *startCommandLog(stderr, args)
		}
	}
	// Let Cobra resolve help/version normally. Opening the log only when work starts
	// keeps informational commands usable without a writable home directory.
	logCommandHandlers(root, start)
	defer run.close()
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(logStream{output: stdout, run: run, name: "stdout"})
	root.SetErr(logStream{output: stderr, run: run, name: "stderr"})
	command, err := root.ExecuteContextC(context.WithValue(ctx, commandLogKey{}, run))
	code := 0
	if err != nil {
		start()
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
	if run.logger != nil {
		run.finish(name, code, err)
	}
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
	flags.DurationVar(&opts.timeout, "timeout", 2*time.Minute, "command deadline")
	_ = root.MarkPersistentFlagDirname("repo")
	root.AddCommand(newDiffCommand(opts), newSnapshotCommand(opts), newVersionCommand(opts, root.Version))
	return root
}

func logCommandHandlers(command *cobra.Command, start func()) {
	if command.Name() == "version" && command.Parent() != nil && command.Parent().Parent() == nil {
		return
	}
	if run := command.RunE; run != nil {
		command.RunE = func(cmd *cobra.Command, args []string) error {
			start()
			return run(cmd, args)
		}
	}
	if run := command.Run; run != nil {
		command.Run = func(cmd *cobra.Command, args []string) {
			start()
			run(cmd, args)
		}
	}
	for _, child := range command.Commands() {
		logCommandHandlers(child, start)
	}
}
