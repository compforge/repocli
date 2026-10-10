package cli

import (
	"context"
	"fmt"

	"github.com/compforge/go-stdx/timeline"
	"github.com/compforge/repocli/toolkit/go"
	"github.com/spf13/cobra"
)

func newDiffCommand(opts *options) *cobra.Command   { return newComparisonCommand(opts, false) }
func newImpactCommand(opts *options) *cobra.Command { return newComparisonCommand(opts, true) }

func newComparisonCommand(opts *options, impact bool) *cobra.Command {
	var base, head, patchFile string
	var staged, showUnits bool
	var maxFiles, maxNodes, maxRelations int
	var maxSnapshotBytes int64
	var unitOptions repocli.UnitOptions
	var testDirs, changedFiles []string
	command := &cobra.Command{
		Use:   "diff",
		Short: "Show captured changes, optionally forming Units",
		Long: `Capture an exact Git comparison without graph or impact analysis.
The target is the working tree (including untracked files), index, commit, or an
in-memory patch postimage. Add --units to form Units from the captured diff.`,
		Example: `  repocli diff --repo /path/to/repo --base main --json
  repocli diff --base main --units --max-units 8`,
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
			return (repocli.DiffRequest{TestDirs: testDirs, MaxFiles: maxFiles, MaxNodes: maxNodes, MaxRelations: maxRelations, MaxSnapshotBytes: maxSnapshotBytes}).Validate()
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
				MaxFiles: maxFiles, MaxNodes: maxNodes, MaxRelations: maxRelations, MaxSnapshotBytes: maxSnapshotBytes,
			}
			if !impact {
				captured, err := repocli.Diff(ctx, request)
				if err != nil {
					return executionError{err}
				}
				if !showUnits {
					return writeRawDiff(command.OutOrStdout(), captured, opts.json)
				}
				result, err := repocli.FormUnits(ctx, captured, unitOptions)
				if err != nil {
					return executionError{err}
				}
				if err := writeUnits(command.OutOrStdout(), result, opts.json); err != nil {
					return executionError{err}
				}
				return nil
			}
			result, err := repocli.AnalyzeImpact(ctx, request)
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
	if impact {
		command.Use = "impact"
		command.Short = "Analyze affected files and recommend tests"
		command.Long = "Analyze a Git comparison using bounded code graphs. Results are static estimates, not test verdicts."
		command.Example = "repocli impact --base main --test-dir tests --json"
	}
	flags := command.Flags()
	if !impact {
		flags.BoolVar(&showUnits, "units", false, "show diff -> Fragment -> Unit formation")
		flags.BoolVar(&unitOptions.FileOnly, "unit-files-only", false, "keep file groups without dependency grouping (with --units)")
		flags.IntVar(&unitOptions.MaxUnits, "max-units", 0, "soft Unit count ceiling; 0 groups only evidenced relations (with --units)")
		flags.IntVar(&unitOptions.MaxFiles, "unit-max-files", 5, "maximum files in a merged Unit (with --units)")
		flags.Int64Var(&unitOptions.MaxChangedLines, "unit-max-lines", 300, "maximum changed lines in a merged Unit (with --units)")
		flags.IntVar(&unitOptions.MaxDiffSize, "unit-max-bytes", 32000, "maximum diff bytes in a merged Unit (with --units)")
	}
	flags.StringVar(&base, "base", "HEAD", "exact base commit/ref")
	flags.StringVar(&head, "head", "", "compare against this commit/ref instead of the working tree")
	flags.BoolVar(&staged, "staged", false, "compare against the index")
	flags.StringArrayVar(&changedFiles, "changed-file", nil, "select changed paths, retaining captured dependency sources (repeatable)")
	flags.StringVar(&patchFile, "file", "", "Git patch file, or - for stdin")
	// StringArray preserves a directory containing commas as one path.
	if impact {
		flags.Int64Var(&maxSnapshotBytes, "max-snapshot-bytes", 0, "maximum captured source bytes per snapshot (0 uses 128 MiB)")
		flags.IntVar(&maxFiles, "max-files", repocli.DefaultImpactMaxFiles, "maximum captured files and parsed source files per snapshot (0 uses default)")
		flags.IntVar(&maxNodes, "max-nodes", repocli.DefaultImpactMaxNodes, "maximum source graph nodes per snapshot (0 uses default)")
		flags.IntVar(&maxRelations, "max-relations", repocli.DefaultImpactMaxRelations, "maximum source graph relations per snapshot (0 uses default)")
		flags.StringArrayVar(&testDirs, "test-dir", nil, "test directory relative to repo root (repeatable)")
		_ = command.MarkFlagDirname("test-dir")
	}
	_ = command.MarkFlagFilename("file")
	command.MarkFlagsMutuallyExclusive("head", "staged", "file")
	return command
}
