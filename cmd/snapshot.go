package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/compforge/repocli/internal/analysis"
	"github.com/spf13/cobra"
)

func newSnapshotCommand(opts *options) *cobra.Command {
	var head string
	var staged bool
	command := &cobra.Command{
		Use:   "snapshot",
		Short: "Identify repository contents without change analysis",
		Long: `Read a content digest of tracked and non-ignored untracked working-tree files.
Use --staged for the index, or --head for an exact commit/ref. No commit, backup,
or filesystem snapshot is created. Includes repository identity and component metadata.
Always check capture completeness before comparing digests.`,
		Example: `  repocli snapshot --json
  repocli snapshot --staged --json
  repocli snapshot --head HEAD --json`,
		Args: cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if opts.timeout <= 0 {
				return fmt.Errorf("--timeout must be positive")
			}
			return nil
		},
		RunE: func(command *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(command.Context(), opts.timeout)
			defer cancel()
			result, err := analysis.CaptureSnapshot(ctx, analysis.SnapshotRequest{Repository: opts.repository, Head: head, Staged: staged})
			if err != nil {
				return executionError{err}
			}
			if err := writeSnapshot(command.OutOrStdout(), result, opts.json); err != nil {
				return executionError{err}
			}
			return nil
		},
	}
	command.Flags().StringVar(&head, "head", "", "read this exact commit/ref instead of the working tree")
	command.Flags().BoolVar(&staged, "staged", false, "read the index instead of the working tree")
	command.MarkFlagsMutuallyExclusive("head", "staged")
	return command
}

func writeSnapshot(output io.Writer, report analysis.SnapshotReport, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	if _, err := fmt.Fprintf(output, "%s input=%s complete=%t files=%d\n", report.Snapshot, report.Input, report.Complete, report.FileCount); err != nil {
		return err
	}
	if report.Repository != nil {
		if _, err := fmt.Fprintf(output, "  repository %s/%s\n", report.Repository.Forge.Name, report.Repository.Path); err != nil {
			return err
		}
	}
	for _, component := range report.Components {
		if _, err := fmt.Fprintf(output, "  component %s (%s, %s)\n", component.Name, component.Root, component.Language); err != nil {
			return err
		}
		for _, tool := range component.PackageTools {
			if _, err := fmt.Fprintf(output, "    package tool %s %s [%s]\n", tool.Name, tool.Version, strings.Join(tool.Evidence, ", ")); err != nil {
				return err
			}
		}
	}
	for _, diagnostic := range report.Diagnostics {
		if _, err := fmt.Fprintf(output, "  %s: %s\n", diagnostic.Code, diagnostic.Message); err != nil {
			return err
		}
	}
	return nil
}
