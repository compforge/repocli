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
		fmt.Fprintf(&buffer, "  component %s (%s, %s)\n", component.Component.Name, component.Root, component.Component.Language)
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
	for _, diagnostic := range result.Diagnostics {
		fmt.Fprintf(&buffer, "  %s: %s\n", diagnostic.Code, diagnostic.Message)
	}
	_, err := io.WriteString(stdout, buffer.String())
	return err
}
