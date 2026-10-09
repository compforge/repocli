package repocli

import (
	"context"

	"github.com/compforge/repocli/toolkit/go/internal/analysis"
)

// DeadcodeRequest selects the snapshot and optional node/relation budgets.
type DeadcodeRequest = analysis.DeadcodeRequest

// DeadcodeReport lists declaration nodes without incoming usage edges, alongside
// snapshot identity and graph diagnostics. Candidates are not deletion verdicts.
type DeadcodeReport = analysis.DeadcodeReport

// AnalyzeDeadcode builds a graph of all captured text documents and finds
// declarations without incoming non-structural edges. It retains all confidence
// levels and self-references, without entrypoint or public-API exclusions.
func AnalyzeDeadcode(ctx context.Context, req DeadcodeRequest) (DeadcodeReport, error) {
	return analysis.AnalyzeDeadcode(ctx, req)
}
