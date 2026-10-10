package impact

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	shared "github.com/compforge/codegraph"
	"github.com/compforge/go-stdx/timeline"
	"github.com/compforge/repocli/toolkit/go/internal/codegraph"
	"github.com/compforge/repocli/toolkit/go/internal/observation"
)

const DefaultMaxFiles = 10000

// Impact may include every supported source file; its node budget must not
// inherit the smaller general-purpose CodeGraph default.
const DefaultMaxNodes = 250000
const DefaultMaxRelations = 500000

// buildWorksets chooses consumer-owned roots; the builder expands repository
// dependencies into documents. A workset is bounded, not a promised closure.
// +spec=`Before and after documents never enter the same graph`
func buildWorksets(ctx context.Context, req Request, testset []string) ([]codegraph.BuildResult, error) {
	if req.MaxFiles == 0 {
		req.MaxFiles = DefaultMaxFiles
	}
	if req.MaxNodes == 0 {
		req.MaxNodes = DefaultMaxNodes
	}
	if req.MaxRelations == 0 {
		req.MaxRelations = DefaultMaxRelations
	}
	ref, _ := timeline.StageFromContext(ctx)
	kinds := append(append([]codegraph.Kind{}, impactKinds...), codegraph.ConfigScope)
	// Share only detached file facts for this diff. Each side still binds its
	// own relationships against its own catalog, including changed dependencies.
	cache, err := shared.NewExtractionCache(4000, 64<<20)
	if err != nil {
		return nil, err
	}
	extractor, err := shared.NewExtractor(shared.ExtractionOptions{Cache: cache})
	if err != nil {
		return nil, err
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
		version := "before"
		if side == 1 {
			version = "after"
		}
		buildCtx, stage := observation.Begin(ctx, ref.TimelineID, "workset."+version, timeline.WithParent(ref.StageID))
		built, err := codegraph.Build(buildCtx, codegraph.BuildRequest{
			BuildOptions: codegraph.BuildOptions{Files: catalog, Resources: resources, Gitlinks: req.Gitlinks,
				Kinds: kinds, MaxDepth: 32, MaxFiles: req.MaxFiles, MaxNodes: req.MaxNodes, MaxRelations: req.MaxRelations, Extractor: extractor},
			FilesToExpand: append(changeset, testset...),
		})
		stage.End(err, timeline.WithEndAttributes(
			timeline.Attribute{Key: "parsedFiles", Value: len(built.ParsedFiles)},
			timeline.Attribute{Key: "diagnostics", Value: len(built.Diagnostics)}))
		if err != nil {
			if errors.Is(err, shared.ErrBuildBudget) {
				var memory runtime.MemStats
				runtime.ReadMemStats(&memory)
				return nil, fmt.Errorf("%s workset: %w; parsed files=%d, MaxFiles=%d, MaxNodes=%d, MaxRelations=%d; current process Go heap allocation=%.1f MiB (not RSS or peak); increase only the exhausted budget and retry", version, err, len(built.ParsedFiles), req.MaxFiles, req.MaxNodes, req.MaxRelations, float64(memory.HeapAlloc)/(1<<20))
			}
			return nil, err
		}
		builds = append(builds, built)
	}
	return builds, nil
}
