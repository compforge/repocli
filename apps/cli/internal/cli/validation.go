package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	repocli "github.com/compforge/repocli/toolkit/go"
	"github.com/spf13/cobra"
)

func newValidationCommand(opts *options, name string) *cobra.Command {
	var components, files []string
	var base string
	var planOnly bool
	command := &cobra.Command{
		Use:   name,
		Short: "Run Makefile-defined Component " + name + " checks",
		Long:  "Run explicit Makefile checks. Without --base, check selected Components in full.\nWith --base, plan from impact evidence. LINT_FILES/TEST_FILES require the documented\nMakefile contract. Missing targets are unavailable; no language commands are inferred.\nPreparation and formatting must finish before validation. Target output is kept in the report.",
		Args:  cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			if opts.timeout <= 0 {
				return errors.New("--timeout must be positive")
			}
			if len(files) > 0 && base != "" {
				return errors.New("--file and --base are mutually exclusive")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), opts.timeout)
			defer cancel()
			request := repocli.ValidationRequest{Repository: opts.repository, Components: components, Files: files}
			if name != "validate" {
				request.Checks = []string{name}
			}
			if base != "" {
				impact, err := repocli.AnalyzeImpact(ctx, repocli.DiffRequest{Repository: opts.repository, Base: base})
				if err != nil {
					return executionError{err}
				}
				request.Impact = &impact
			}
			plan, err := repocli.PlanValidation(ctx, request)
			if err != nil {
				return executionError{err}
			}
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			if planOnly {
				return encoder.Encode(plan)
			}
			report, err := repocli.ExecuteValidation(ctx, plan)
			if err != nil {
				return executionError{err}
			}
			if opts.json {
				if err := encoder.Encode(report); err != nil {
					return executionError{err}
				}
			} else {
				for _, group := range report.Components {
					for _, check := range group.Checks {
						fmt.Fprintf(cmd.OutOrStdout(), "%s %s: %s (%s, %.2fs) — %s\n", group.Component.Root, check.Check, check.Status, check.Scope, check.DurationSeconds, check.Reason)
						if check.Stdout != "" {
							fmt.Fprint(cmd.OutOrStdout(), check.Stdout)
						}
						if check.Stderr != "" {
							fmt.Fprint(cmd.ErrOrStderr(), check.Stderr)
						}
						if check.Diagnostic != "" {
							fmt.Fprintln(cmd.ErrOrStderr(), check.Diagnostic)
						}
					}
				}
				for _, diagnostic := range report.Diagnostics {
					fmt.Fprintln(cmd.ErrOrStderr(), diagnostic)
				}
			}
			if !report.Complete || !report.Passed {
				return executionError{errors.New("validation did not establish a complete pass")}
			}
			return nil
		},
	}
	command.Flags().StringSliceVar(&components, "component", nil, "Component root(s); default: all discovered Components")
	command.Flags().StringArrayVar(&files, "file", nil, "repository-relative file candidate (repeatable)")
	command.Flags().StringVar(&base, "base", "", "analyze working-tree impact against this revision")
	command.Flags().BoolVar(&planOnly, "plan", false, "print the plan as JSON without running checks")
	return command
}
