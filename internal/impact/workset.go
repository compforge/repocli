package impact

import (
	"context"

	"github.com/compforge/repocli/internal/codegraph"
)

// buildWorksets chooses consumer-owned roots; the builder expands repository
// dependencies into documents. A workset is bounded, not a promised closure.
// +spec=`Before and after documents never enter the same graph`
func buildWorksets(ctx context.Context, req Request, testset []string) ([]codegraph.BuildResult, error) {
	kinds := []codegraph.Kind{}
	if len(req.TestDirs) != 0 {
		kinds = append(kinds, codegraph.Imports, codegraph.Reexports, codegraph.PackageMember, codegraph.ConfigExtends, codegraph.ConfigScope)
		if req.Mode != "file" {
			kinds = append(kinds, codegraph.Contains, codegraph.Calls)
		}
	}
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
		resources := req.BeforeResources
		if side == 1 {
			resources = req.AfterResources
		}
		built, err := codegraph.Build(ctx, codegraph.BuildRequest{
			BuildOptions: codegraph.BuildOptions{Files: catalog, Resources: resources, Gitlinks: req.Gitlinks,
				Kinds: kinds, MaxDepth: 32, MaxFiles: 2000},
			FilesToExpand: append(changeset, testset...),
		})
		if err != nil {
			return nil, err
		}
		builds = append(builds, built)
	}
	return builds, nil
}
