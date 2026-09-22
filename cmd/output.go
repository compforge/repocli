package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/compforge/repocli/internal/analysis"
)

func writeReport(stdout io.Writer, result analysis.Report, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	var buffer strings.Builder
	fmt.Fprintf(&buffer, "%d changed files; test scope: %s\n", len(result.Changes), result.Scope)
	for _, component := range result.Components {
		fmt.Fprintf(&buffer, "  component %s (%s, %s), scope=%s complete=%t\n", component.Component.Name, component.Root, component.Component.Language, component.Scope, component.Complete)
		for _, reason := range component.FallbackReasons {
			fmt.Fprintf(&buffer, "    incomplete: %s\n", reason)
		}
	}
	for _, c := range result.Changes {
		fmt.Fprintf(&buffer, "  %s %s\n", c.Status, c.Path)
		for _, s := range c.Before {
			fmt.Fprintf(&buffer, "    - %s %s (%d-%d)\n", s.Kind, s.Name, s.StartLine, s.EndLine)
		}
		for _, s := range c.After {
			fmt.Fprintf(&buffer, "    + %s %s (%d-%d)\n", s.Kind, s.Name, s.StartLine, s.EndLine)
		}
	}
	for _, reason := range result.Reasons {
		fmt.Fprintf(&buffer, "  test %s [%s]\n", reason.TestFile, reason.Kind)
		if len(reason.DependencyPath) > 0 {
			fmt.Fprintf(&buffer, "    %v\n", reason.DependencyPath)
		}
	}
	for _, name := range result.SourceFiles {
		fmt.Fprintln(&buffer, "  source", name)
	}
	if len(result.Diagnostics) > 0 {
		fmt.Fprintln(&buffer, "  Analysis gaps may hide additional affected tests; listed tests retain their evidence.")
	}
	for _, diagnostic := range result.Diagnostics {
		location := diagnostic.Path
		if location == "" {
			location = "location unavailable"
		}
		if diagnostic.Line > 0 {
			location += fmt.Sprintf(":%d", diagnostic.Line)
		}
		fields := []string{diagnostic.Code}
		for _, field := range []struct{ name, value string }{
			{"reason", diagnostic.Reason}, {"relation", diagnostic.Relation}, {"version", diagnostic.Version},
		} {
			if field.value != "" {
				fields = append(fields, field.name+"="+field.value)
			}
		}
		fmt.Fprintf(&buffer, "  %s [%s]: %s\n", location, strings.Join(fields, ", "), diagnostic.Message)
	}
	for _, observation := range result.Observations {
		fmt.Fprintf(&buffer, "  observation (outside selected test dependencies) %s: %s\n", observation.Path, observation.Message)
	}
	_, err := io.WriteString(stdout, buffer.String())
	return err
}
