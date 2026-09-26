package cmd

import (
	"context"
	"fmt"

	"github.com/compforge/go-stdx/timeline"
	"github.com/compforge/repocli/internal/analysis"
	"github.com/compforge/repocli/internal/impact"
	"github.com/spf13/cobra"
)

func newDiffCommand(opts *options) *cobra.Command {
	var base, head, patchFile string
	var staged bool
	var testDirs, changedFiles []string
	command := &cobra.Command{
		Use:   "diff",
		Short: "Describe changes and potentially affected files",
		Long: `Compare an exact base commit with the working tree, including staged,
unstaged, and non-ignored untracked files. The default base is HEAD.

With --file, reconstruct the patch postimage from the base in memory. Supply
the commit the patch was generated against; working-tree contents are not used.

Test directories are relative to the repository root. Without them, all supported sources
are candidate roots. Granularity is selected automatically. Impact is a static
estimate; confidence describes path evidence, not a probability or test verdict.`,
		Example: `  repocli diff --repo /path/to/repo --base main --test-dir tests --json
  git diff --binary HEAD | repocli diff --file - --test-dir tests --json`,
		Args: cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if opts.timeout <= 0 {
				return fmt.Errorf("--timeout must be positive")
			}
			dirs, err := impact.ValidateDirs(testDirs)
			if err != nil {
				return err
			}
			testDirs = dirs
			return nil
		},
		RunE: func(command *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(command.Context(), opts.timeout)
			defer cancel()
			operation := timeline.New("diff.analysis")
			ctx = timeline.NewContext(ctx, operation)
			request := analysis.Request{
				Repository: opts.repository, Base: base, PatchFile: patchFile,
				TestDirs: testDirs, Stdin: command.InOrStdin(),
				Head: head, Staged: staged, ChangedFiles: changedFiles,
			}
			result, err := analysis.Analyze(ctx, request)
			// Preserve the timeline when analysis ends at its deadline. The result
			// history is also written before stdout, as for successful analyses.
			recordDiff(command.Context(), request, result, opts.timeout, operation.Finish(), err)
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
	flags.StringArrayVar(&changedFiles, "changed-file", nil, "restrict changed seeds, retaining the full dependency graph (repeatable)")
	flags.StringVar(&patchFile, "file", "", "Git patch file, or - for stdin")
	// StringArray preserves a directory containing commas as one path.
	flags.StringArrayVar(&testDirs, "test-dir", nil, "test directory relative to repo root (repeatable)")
	_ = command.MarkFlagDirname("test-dir")
	_ = command.MarkFlagFilename("file")
	command.MarkFlagsMutuallyExclusive("head", "staged", "file")
	return command
}
