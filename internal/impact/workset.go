package impact

import (
	"context"
	"time"

	"github.com/compforge/go-stdx/timeline"
	"github.com/compforge/repocli/internal/codegraph"
)

// buildWorksets chooses consumer-owned roots; the builder expands repository
// dependencies into documents. A workset is bounded, not a promised closure.
// +spec=`Before and after documents never enter the same graph`
func buildWorksets(ctx context.Context, req Request, testset []string) ([]codegraph.BuildResult, error) {
	kinds := append(append([]codegraph.Kind{}, impactKinds...), codegraph.ConfigScope)
	var builds []codegraph.BuildResult
	for side, catalog := range []map[string][]byte{req.Before, req.After} {
		var changeset []string
		for _, change := range req.Changes {
			name := change.Path
			if side == 0 && change.OldPath != "" {
				name = change.OldPath
			}
			changeset = append(changeset, name)
		}
		// Without a candidate directory, expose affected source files across the
		// repository, within the same explicit expansion budget.
		if len(req.TestDirs) == 0 {
			for name := range catalog {
				if codegraph.Language(name) != "" {
					changeset = append(changeset, name)
				}
			}
		}
		resources := req.BeforeResources
		if side == 1 {
			resources = req.AfterResources
		}
		started := time.Now()
		built, err := codegraph.Build(ctx, codegraph.BuildRequest{
			BuildOptions: codegraph.BuildOptions{Files: catalog, Resources: resources, Gitlinks: req.Gitlinks,
				Kinds: kinds, MaxDepth: 32, MaxFiles: 2000},
			FilesToExpand: append(changeset, testset...),
		})
		if operation, ok := timeline.FromContext(ctx); ok {
			version := "before"
			if side == 1 {
				version = "after"
			}
			operation.StepSince(started, "workset."+version,
				timeline.Field{Key: "parsedFiles", Value: len(built.ParsedFiles)},
				timeline.Field{Key: "diagnostics", Value: len(built.Diagnostics)},
				timeline.Field{Key: "failed", Value: err != nil})
		}
		if err != nil {
			return nil, err
		}
		builds = append(builds, built)
	}
	return builds, nil
}
