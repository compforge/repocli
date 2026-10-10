package cli

import (
	"bytes"
	"context"
	"fmt"
	"github.com/compforge/go-stdx/timeline"
	"testing"
)

func TestDiffPathsFinishAndExportTimeline(t *testing.T) {
	for _, flags := range [][]string{{}, {"--units"}, {"--base", "missing-ref"}} {
		t.Run(fmt.Sprint(flags), func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			repo := fixture(t)
			var out, stderr bytes.Buffer
			code := Execute(context.Background(), append([]string{"diff", "--repo", repo}, flags...), nil, &out, &stderr)
			records := readDiffHistory(t, home)
			if len(records) != 1 {
				t.Fatalf("records=%d", len(records))
			}
			got := records[0].Timeline
			want := timeline.Succeeded
			if code != 0 {
				want = timeline.Failed
			}
			if got.Operation != "diff" || got.Status != want || got.FinishedAt.IsZero() || len(got.RunningStages()) != 0 {
				t.Fatalf("unclosed command: %+v", got)
			}
		})
	}
}
