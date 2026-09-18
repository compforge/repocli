package cmd

import (
	"context"
	"fmt"

	"github.com/compforge/repocli/internal/analysis"
	"github.com/compforge/repocli/internal/impact"
	"github.com/spf13/cobra"
)

func newDiffCommand(opts *options) *cobra.Command {
	var base, head, patchFile, mode string
	var staged bool
	var testDirs, changedFiles []string
	command := &cobra.Command{
		Use:   "diff",
		Short: "Describe changes and potentially affected tests",
		Long: `Compare an exact base commit with the working tree, including staged,
unstaged, and non-ignored untracked files. The default base is HEAD.

With --file, reconstruct the patch postimage from the base in memory. Supply
the commit the patch was generated against; working-tree contents are not used.

Test directories are relative to the repository root. Without them, only changes
and symbols are reported. Test impact is a static estimate, not a test verdict.`,
		Example: `  repocli diff --repo /path/to/repo --base main --test-dir tests --json
  git diff --binary HEAD | repocli diff --file - --test-dir tests --json`,
		Args: cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if opts.timeout <= 0 {
				return fmt.Errorf("--timeout must be positive")
			}
			if mode != "file" && mode != "symbol" {
				return fmt.Errorf("--impact must be file or symbol")
			}
			dirs, err := impact.ValidateDirs(testDirs)
			if err != nil {
				return err
			}
			testDirs = dirs
			return nil
		},
		RunE: func(command *cobra.Command, _ []string) (runErr error) {
			ctx, cancel := context.WithTimeout(command.Context(), opts.timeout)
			defer cancel()
			request := analysis.Request{
				Repository: opts.repository, Base: base, PatchFile: patchFile,
				TestDirs: testDirs, Stdin: command.InOrStdin(),
				Head: head, Staged: staged, ChangedFiles: changedFiles, Mode: mode,
			}
			log := startDiffLog(opts.noLog, opts.stderr, request, opts.timeout)
			opts.log = log
			var result analysis.Report
			defer func() { log.finish(ctx, result, runErr) }()
			var err error
			result, err = analysis.Analyze(ctx, request)
			if err != nil {
				return executionError{err}
			}
			if err := writeReport(command.OutOrStdout(), result, opts.json); err != nil {
				return executionError{err}
			}
			return nil
		},
	}
	flags := command.Flags()
	flags.StringVar(&base, "base", "HEAD", "exact base commit/ref")
	flags.StringVar(&head, "head", "", "compare against this commit/ref instead of the working tree")
	flags.BoolVar(&staged, "staged", false, "compare against the index")
	flags.StringVar(&mode, "impact", "symbol", "dependency granularity: symbol or file")
	flags.StringArrayVar(&changedFiles, "changed-file", nil, "restrict changed seeds, retaining the full dependency graph (repeatable)")
	flags.StringVar(&patchFile, "file", "", "Git patch file, or - for stdin")
	// StringArray preserves a directory containing commas as one path.
	flags.StringArrayVar(&testDirs, "test-dir", nil, "test directory relative to repo root (repeatable)")
	_ = command.MarkFlagDirname("test-dir")
	_ = command.MarkFlagFilename("file")
	command.MarkFlagsMutuallyExclusive("head", "staged", "file")
	return command
}
