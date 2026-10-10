package repocli_test

import (
	"context"
	"testing"

	"github.com/compforge/go-stdx/timeline"
	"github.com/compforge/repocli/toolkit/go"
)

func TestLibraryContributesToCallerTimeline(t *testing.T) {
	m, err := timeline.NewManager(nil, timeline.Config{})
	if err != nil {
		t.Fatal(err)
	}
	previous := timeline.SetDefault(m)
	defer timeline.SetDefault(previous)
	defer m.Shutdown(context.Background())
	const id = "caller-session"
	if err := timeline.Start(id, "review"); err != nil {
		t.Fatal(err)
	}
	parent, err := timeline.Begin(id, "diff.load")
	if err != nil {
		t.Fatal(err)
	}
	ctx := timeline.NewStageContext(context.Background(), timeline.StageRef{TimelineID: id, StageID: parent.ID()})
	repo := fixture(t)
	put(t, repo, "main.go", "package main\nfunc main() { println(1) }\n")
	captured, err := repocli.Diff(ctx, repocli.DiffRequest{Repository: repo})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repocli.FormUnits(ctx, captured, repocli.UnitOptions{FileOnly: true}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := timeline.Read(context.Background(), id, false)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != timeline.Unknown || len(snapshot.Stages) != 3 {
		t.Fatalf("library finished caller or lost stages: %+v", snapshot)
	}
	for _, stage := range snapshot.Stages {
		if stage.ID == parent.ID() {
			if stage.Status != timeline.Running {
				t.Fatal("library ended caller stage")
			}
		} else if stage.ParentID != parent.ID() || stage.Status != timeline.Succeeded {
			t.Fatalf("library stage escaped explicit parent: %+v", stage)
		}
	}
	parent.End(nil)
}

func TestLibraryWorksWithoutTimelineSetup(t *testing.T) {
	previous := timeline.SetDefault(nil)
	defer timeline.SetDefault(previous)
	repo := fixture(t)
	if _, err := repocli.Diff(context.Background(), repocli.DiffRequest{Repository: repo}); err != nil {
		t.Fatal(err)
	}
}
