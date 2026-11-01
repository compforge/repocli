package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	repocli "github.com/compforge/repocli/toolkit/go"
	"github.com/spf13/cobra"
)

func newValidationCommand(opts *options, name string) *cobra.Command {
	var components, files []string
	var base string
	var planOnly bool
	var concurrency int
	var inputsFile, output string
	var reuse, changed, checks []string
	command := &cobra.Command{
		Use:   name,
		Short: "Run Makefile-defined Component " + name + " checks",
		Long:  "Run explicit Makefile checks. Without --base, check selected Components in full.\nWith --base, plan from impact evidence. LINT_FILES/TEST_FILES require the documented\nMakefile contract. Missing targets are unavailable; no language commands are inferred.\nUse validate --check fix,lint,test to fix first and validate the resulting contents. Target output is kept in the report.",
		Args:  cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			if opts.timeout <= 0 {
				return errors.New("--timeout must be positive")
			}
			if len(changed) > 0 && base == "" {
				return errors.New("--changed-file requires --base")
			}
			if len(files) > 0 && base != "" {
				return errors.New("--file and --base are mutually exclusive")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), opts.timeout)
			defer cancel()
			request := repocli.ValidationRequest{Repository: opts.repository, Components: components, Files: files, Concurrency: concurrency, Checks: checks, Base: base, ChangedFiles: changed}
			if inputsFile != "" {
				data, err := os.ReadFile(inputsFile)
				if err != nil {
					return executionError{err}
				}
				decoder := json.NewDecoder(bytes.NewReader(data))
				decoder.DisallowUnknownFields()
				if err := decoder.Decode(&request.Inputs); err != nil {
					return executionError{err}
				}
			}
			for _, path := range reuse {
				data, err := os.ReadFile(path)
				if err != nil {
					return executionError{err}
				}
				var previous repocli.ValidationReport
				if err := json.Unmarshal(data, &previous); err != nil {
					return executionError{err}
				}
				if previous.SchemaVersion != 1 {
					return executionError{errors.New("unsupported validation report schema")}
				}
				request.Previous = append(request.Previous, previous)
			}
			if name != "validate" {
				request.Checks = []string{name}
			}
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			if planOnly {
				plan, err := repocli.PlanValidation(ctx, request)
				if err != nil {
					return executionError{err}
				}
				return encoder.Encode(plan)
			}
			report, err := repocli.Validate(ctx, request)
			if err != nil {
				return executionError{err}
			}
			if output != "" {
				if err := writeValidationReport(output, report); err != nil {
					return executionError{err}
				}
			}
			if opts.json {
				if err := encoder.Encode(report); err != nil {
					return executionError{err}
				}
			} else {
				for _, group := range append(report.Preparation, report.Components...) {
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
			if (!report.InputsConsistent && len(report.Components) > 0) || !report.Complete || !report.Passed {
				return executionError{errors.New("validation did not establish a complete pass")}
			}
			return nil
		},
	}
	command.Flags().StringSliceVar(&components, "component", nil, "Component root(s); default: all discovered Components")
	command.Flags().StringArrayVar(&files, "file", nil, "repository-relative file candidate (repeatable)")
	command.Flags().StringVar(&base, "base", "", "analyze working-tree impact against this revision")
	command.Flags().BoolVar(&planOnly, "plan", false, "print the plan as JSON without running checks")
	command.Flags().IntVar(&concurrency, "concurrency", 2, "maximum concurrent checks (1–64)")
	command.Flags().StringVar(&inputsFile, "inputs", "", "JSON Component/check input declarations and receipt paths")
	command.Flags().StringArrayVar(&reuse, "reuse", nil, "previous ValidationReport file (repeatable)")
	command.Flags().StringArrayVar(&changed, "changed-file", nil, "restrict impact seeds to a changed repository-relative path")
	command.Flags().StringVar(&output, "output", "", "atomically save the ValidationReport to this file")
	if name == "validate" {
		command.Flags().StringSliceVar(&checks, "check", nil, "steps to run (fix,lint,test); fix runs first, default lint,test")
	}
	return command
}

func writeValidationReport(path string, report repocli.ValidationReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".validation-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
