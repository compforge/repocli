package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/compforge/repocli/internal/analysis"
	"github.com/compforge/repocli/internal/impact"
)

func writeReport(stdout io.Writer, result analysis.Report, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	var buffer strings.Builder
	fmt.Fprintf(&buffer, "%d changed files; impact scope: %s\n", len(result.Changes), result.Scope)
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
	for _, file := range result.AffectedFiles {
		fmt.Fprintf(&buffer, "  affected %s [confidence=%s distance=%d version=%s]\n", file.Path, file.Confidence, file.Distance, file.Version)
		fmt.Fprintf(&buffer, "    seed %s (%s, %s)\n", file.Seed.ID, file.Seed.Granularity, file.Seed.Basis)
		fmt.Fprintf(&buffer, "    %v\n", file.DependencyPath)
	}
	for _, test := range result.TestFiles {
		fmt.Fprintln(&buffer, "  test", test)
	}
	for _, name := range result.SourceFiles {
		fmt.Fprintln(&buffer, "  source", name)
	}
	if len(result.Diagnostics) > 0 {
		fmt.Fprintln(&buffer, "  Analysis gaps may hide additional affected files; listed files retain their evidence.")
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
	writeObservations(&buffer, result.Observations)
	_, err := io.WriteString(stdout, buffer.String())
	return err
}

// Local extraction gaps can contain thousands of unresolved calls. Keep the
// full locations in JSON and show counts plus bounded examples in readable output.
func writeObservations(buffer *strings.Builder, observations []impact.Uncertainty) {
	groups := map[string][]impact.Uncertainty{}
	for _, observation := range observations {
		if observation.Disposition != "local_gap" {
			fmt.Fprintf(buffer, "  observation %s [%s]: %s\n", observation.Path, observation.Reason, observation.Message)
			continue
		}
		key := observation.Reason + " (" + string(observation.Subject) + ")"
		groups[key] = append(groups[key], observation)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		group := groups[key]
		fmt.Fprintf(buffer, "  local gap %s: %d occurrences\n", key, len(group))
		for _, observation := range group[:min(3, len(group))] {
			fmt.Fprintf(buffer, "    %s:%d [%s]: %s\n", observation.Path, observation.Line, observation.Version, observation.Message)
		}
		if len(group) > 3 {
			fmt.Fprintf(buffer, "    ... %d more; use --json for full locations\n", len(group)-3)
		}
	}
}
