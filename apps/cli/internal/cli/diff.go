package cli

import (
	"context"
	"fmt"

	"github.com/compforge/go-stdx/timeline"
	"github.com/compforge/repocli/toolkit/go"
	"github.com/spf13/cobra"
)

func newDiffCommand(opts *options) *cobra.Command {
	var base, head, patchFile string
	var staged, showUnits bool
	var unitOptions repocli.UnitOptions
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
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			for _, flag := range []string{"unit-files-only", "max-units", "unit-max-files", "unit-max-lines", "unit-max-bytes"} {
				if cmd.Flags().Changed(flag) && !showUnits {
					return fmt.Errorf("--%s requires --units", flag)
				}
			}
			if opts.timeout <= 0 {
				return fmt.Errorf("--timeout must be positive")
			}
			if showUnits && len(testDirs) > 0 {
				return fmt.Errorf("--units and --test-dir cannot be combined")
			}
			if unitOptions.MaxUnits < 0 || unitOptions.MaxDiffSize < 0 || unitOptions.MaxChangedLines < 0 || unitOptions.MaxFiles < 0 {
				return fmt.Errorf("unit limits must not be negative")
			}
			return (repocli.DiffRequest{TestDirs: testDirs}).Validate()
		},
		RunE: func(command *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(command.Context(), opts.timeout)
			defer cancel()
			operation := startDiffTimeline(ctx)
			ctx = timeline.NewContext(ctx, operation)
			request := repocli.DiffRequest{
				Repository: opts.repository, Base: base, PatchFile: patchFile,
				TestDirs: testDirs, Stdin: command.InOrStdin(),
				Head: head, Staged: staged, ChangedFiles: changedFiles,
			}
			if showUnits {
				result, err := repocli.DiffUnits(ctx, request, unitOptions)
				if err != nil {
					return executionError{err}
				}
				if err := writeUnits(command.OutOrStdout(), result, opts.json); err != nil {
					return executionError{err}
				}
				return nil
			}
			result, err := repocli.Diff(ctx, request)
			// Preserve the timeline when analysis ends at its deadline. The result
			// history is also written before stdout, as for successful analyses.
			recordDiff(command.Context(), request, result, opts.timeout, operation, err)
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
	flags.BoolVar(&showUnits, "units", false, "show diff -> Fragment -> Unit formation instead of impact analysis")
	flags.BoolVar(&unitOptions.FileOnly, "unit-files-only", false, "keep file groups without dependency grouping (with --units)")
	flags.IntVar(&unitOptions.MaxUnits, "max-units", 0, "preferred Unit count; 0 leaves count unconstrained (with --units)")
	flags.IntVar(&unitOptions.MaxFiles, "unit-max-files", 5, "maximum files in a merged Unit (with --units)")
	flags.Int64Var(&unitOptions.MaxChangedLines, "unit-max-lines", 300, "maximum changed lines in a merged Unit (with --units)")
	flags.IntVar(&unitOptions.MaxDiffSize, "unit-max-bytes", 32000, "maximum diff bytes in a merged Unit (with --units)")
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
