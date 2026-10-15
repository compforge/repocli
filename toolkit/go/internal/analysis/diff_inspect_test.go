package analysis

import (
	"github.com/compforge/repocli/toolkit/go/internal/git"
	"testing"
)

func TestDiffInspectReportsUnavailableMetadata(t *testing.T) {
	d := DiffReport{AfterSnapshot: "captured", captured: &comparison{after: git.Snapshot{Files: map[string][]byte{"main.ts": nil}, Opaque: map[string]string{"package.json": "large"}}}}
	report, err := d.Inspect(false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Complete || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "metadata_unavailable" {
		t.Fatalf("report: %+v", report)
	}
	if owner := report.Owner("main.ts"); owner == nil || owner.Root != "." {
		t.Fatalf("opaque marker lost: %+v", owner)
	}
	report.Components[0].Manifests[0] = "mutated"
	again, err := d.Inspect(false)
	if err != nil || again.Components[0].Manifests[0] != "package.json" {
		t.Fatalf("caller mutation affected capture: %+v %v", again, err)
	}
}
