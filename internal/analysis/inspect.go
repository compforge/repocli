package analysis

import (
	"context"
	"reflect"

	"github.com/compforge/repocli/internal/project"
)

// InspectReport describes repository organization without a full-content digest.
// Complete covers the file catalog and metadata observation, not source parsing,
// buildability, or impact coverage. Head identifies only committed input.
type InspectReport struct {
	project.Layout
	SchemaVersion int          `json:"schemaVersion"`
	Checkout      string       `json:"checkout"`
	Input         string       `json:"input"`
	Head          string       `json:"head,omitempty"`
	Complete      bool         `json:"complete"`
	Diagnostics   []Diagnostic `json:"diagnostics"`
}

// Inspect reads only layout metadata; source contents and submodule contents are
// outside its read set. It shares the exact layout policy used by captured inputs.
// +spec=`Repository inspection does not require source capture or code graph construction`
func Inspect(ctx context.Context, req InputRequest) (InspectReport, error) {
	repo, input, head, err := selectInput(ctx, req)
	if err != nil {
		return InspectReport{}, err
	}
	files, err := repo.Catalog(ctx, head, req.Staged, project.NeedsContent)
	if err != nil {
		return InspectReport{}, err
	}
	origin, err := repo.Origin(ctx)
	if err != nil {
		return InspectReport{}, err
	}
	layout, err := project.Load(files, origin)
	if err != nil {
		return InspectReport{}, err
	}
	report := InspectReport{Layout: layout, SchemaVersion: 1, Checkout: repo.Root, Input: input, Head: head, Complete: true, Diagnostics: []Diagnostic{}}
	if head == "" {
		latest, err := repo.Catalog(ctx, head, req.Staged, project.NeedsContent)
		if err != nil {
			return InspectReport{}, err
		}
		if !reflect.DeepEqual(files, latest) {
			report.Complete = false
			report.Diagnostics = append(report.Diagnostics, Diagnostic{Code: "inspection_changed", Message: "repository paths or layout metadata changed during inspection"})
		}
	}
	return report, nil
}
