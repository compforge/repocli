package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/compforge/repocli/toolkit/go"
	"github.com/spf13/cobra"
)

func newInspectCommand(opts *options) *cobra.Command {
	var head string
	var staged bool
	command := &cobra.Command{
		Use:   "inspect",
		Short: "Describe repository components, languages and package tools",
		Long: `Inspect repository organization from tracked and non-ignored untracked paths.
Read only configuration needed for layout and tool evidence. No source parsing,
full-content digest, or submodule capture is performed. Use snapshot for content identity.`,
		Example: `  repocli inspect --json
  repocli inspect --staged --json
  repocli inspect --head HEAD --json`,
		Args: cobra.NoArgs,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if opts.timeout <= 0 {
				return fmt.Errorf("timeout must be positive")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), opts.timeout)
			defer cancel()
			report, err := repocli.Inspect(ctx, repocli.InputRequest{Repository: opts.repository, Head: head, Staged: staged})
			if err != nil {
				return executionError{err}
			}
			if err := writeInspect(cmd.OutOrStdout(), report, opts.json); err != nil {
				return executionError{err}
			}
			return nil
		},
	}
	command.Flags().StringVar(&head, "head", "", "inspect this exact commit/ref instead of the working tree")
	command.Flags().BoolVar(&staged, "staged", false, "inspect the index instead of the working tree")
	command.MarkFlagsMutuallyExclusive("head", "staged")
	return command
}

func writeInspect(output io.Writer, report repocli.InspectReport, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	if _, err := fmt.Fprintf(output, "input=%s complete=%t components=%d\n", report.Input, report.Complete, len(report.Components)); err != nil {
		return err
	}
	if err := writeLayout(output, report.Layout); err != nil {
		return err
	}
	for _, diagnostic := range report.Diagnostics {
		if _, err := fmt.Fprintf(output, "  %s: %s\n", diagnostic.Code, diagnostic.Message); err != nil {
			return err
		}
	}
	return nil
}

func writeLayout(output io.Writer, layout repocli.Layout) error {
	if layout.Repository != nil {
		if _, err := fmt.Fprintf(output, "  repository %s/%s\n", layout.Repository.Forge.Name, layout.Repository.Path); err != nil {
			return err
		}
	}
	for _, component := range layout.Components {
		if _, err := fmt.Fprintf(output, "  component %s (%s, %s)\n", component.Name, component.Root, component.Language); err != nil {
			return err
		}
		for _, tool := range component.PackageTools {
			if _, err := fmt.Fprintf(output, "    package tool %s %s [%s]\n", tool.Name, tool.Version, strings.Join(tool.Evidence, ", ")); err != nil {
				return err
			}
		}
	}
	return nil
}
