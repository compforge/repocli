package analysis

import (
	"fmt"
	"sort"

	"github.com/compforge/repocli/toolkit/go/internal/project"
)

// Inspect describes one retained comparison side without reading Git or the filesystem.
// Select does not narrow this repository layout. A deserialized report has no retained input.
// +spec=`Comparison layout and source bytes share one captured snapshot, including deleted and moved component boundaries`
// +link=repo://docs/repository.md
func (d DiffReport) Inspect(before bool) (InspectReport, error) {
	if d.captured == nil {
		return InspectReport{}, fmt.Errorf("inspect diff: no captured source snapshot")
	}
	contents, snapshot, head, input := d.captured.after, d.AfterSnapshot, d.Head, d.Input
	if before {
		contents, snapshot, head, input = d.captured.before, d.BeforeSnapshot, d.Base, "commit"
	}
	files := make(map[string][]byte, len(contents.Files)+len(contents.Opaque))
	for name, data := range contents.Files {
		files[name] = data
	}
	// Even an unavailable body still proves a regular-file marker exists.
	for name := range contents.Opaque {
		files[name] = nil
	}
	layout, err := project.Load(files, d.captured.origin)
	if err != nil {
		return InspectReport{}, fmt.Errorf("inspect snapshot %s: %w", snapshot, err)
	}
	report := InspectReport{Layout: layout, SchemaVersion: 1, Checkout: d.Checkout, Input: input, Head: head, Snapshot: snapshot, Complete: true, Diagnostics: []Diagnostic{}}
	for _, issue := range contents.Issues {
		report.Diagnostics = append(report.Diagnostics, diagnostic("snapshot_incomplete", issue))
	}
	for name := range contents.Opaque {
		if project.NeedsContent(name) {
			report.Diagnostics = append(report.Diagnostics, Diagnostic{Code: "metadata_unavailable", Path: name, Message: "layout metadata exceeds capture limits"})
		}
	}
	for _, issue := range d.Diagnostics {
		if issue.Code == "snapshot_changed" {
			report.Diagnostics = append(report.Diagnostics, Diagnostic{Code: issue.Code, Message: issue.Message})
		}
	}
	sort.SliceStable(report.Diagnostics, func(i, j int) bool {
		if report.Diagnostics[i].Code != report.Diagnostics[j].Code {
			return report.Diagnostics[i].Code < report.Diagnostics[j].Code
		}
		return report.Diagnostics[i].Path < report.Diagnostics[j].Path
	})
	report.Complete = len(report.Diagnostics) == 0
	return report, nil
}
