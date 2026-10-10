// Package observation records optional diagnostics without owning application lifecycles.
package observation

import (
	"context"
	"log/slog"

	"github.com/compforge/go-stdx/timeline"
)

// Begin binds the new stage identity for downstream instrumentation. Callers
// supply parentage explicitly; an absent timeline ID disables recording.
func Begin(ctx context.Context, id, name string, opts ...timeline.StageOption) (context.Context, timeline.StageHandle) {
	if id == "" {
		return ctx, timeline.Noop("").Begin(name)
	}
	stage, err := timeline.Begin(id, name, opts...)
	if err != nil {
		slog.WarnContext(ctx, "repocli: timeline recording failed", "timeline_id", id, "stage", name, "error", err)
		return ctx, timeline.Noop(id).Begin(name)
	}
	return timeline.NewStageContext(ctx, timeline.StageRef{TimelineID: id, StageID: stage.ID()}), stage
}
