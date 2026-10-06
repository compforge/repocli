package analysis

import (
	"context"
	"testing"

	"github.com/compforge/repocli/internal/codegraph"
	"github.com/compforge/repocli/internal/git"
)

func TestManifestPreparationKeepsCaptureAndParseCompletenessSeparate(t *testing.T) {
	contents := git.Snapshot{Files: map[string][]byte{"app/package.json": []byte(`{"name":`), "main.go": []byte("not valid source")}}
	prepared, err := prepareCaptured(context.Background(), contents, "", "", "index", "")
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.Report.Complete || len(prepared.Report.Diagnostics) != 0 || len(prepared.Report.Observations) == 0 {
		t.Fatalf("report: %+v", prepared.Report)
	}
	if _, ok := prepared.Manifests.Document("main.go"); ok {
		t.Fatal("prepare parsed source")
	}
	if len(prepared.Report.Components) != 1 || prepared.Report.Components[0].Root != "app" {
		t.Fatalf("components: %+v", prepared.Report.Components)
	}
}

func TestManifestPreparationIsSnapshotBound(t *testing.T) {
	prepare := func(module string) *PreparedSnapshot {
		t.Helper()
		p, err := prepareCaptured(context.Background(), git.Snapshot{Files: map[string][]byte{"go.mod": []byte("module " + module + "\n")}}, "", "", "commit", "")
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	before, after := prepare("example.com/before"), prepare("example.com/after")
	if before.Report.Snapshot == after.Report.Snapshot || codegraph.GoModules(before.Manifests)["."] != "example.com/before" || codegraph.GoModules(after.Manifests)["."] != "example.com/after" {
		t.Fatal("manifest context crossed snapshot versions")
	}
}
