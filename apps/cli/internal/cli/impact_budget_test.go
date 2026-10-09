package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestImpactBudgetFlagsAndRetry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := fixture(t)
	put(t, repo, "source file.ts", "export function a() { return 9; }\n")
	for _, tc := range []struct {
		flag, value, detail string
		code                int
	}{
		{"--max-files", "-1", "must not be negative", 2},
		{"--max-relations", "-1", "must not be negative", 2},
		{"--max-files", "1", "snapshot file budget exceeded: files=4 limit=1", 1},
		{"--max-relations", "1", "MaxRelations=1", 1},
	} {
		t.Run(tc.flag+tc.value, func(t *testing.T) {
			var out, stderr bytes.Buffer
			code := Execute(context.Background(), []string{"impact", "--repo", repo, tc.flag, tc.value}, nil, &out, &stderr)
			if code != tc.code || !strings.Contains(stderr.String(), tc.detail) {
				t.Fatalf("exit=%d stderr=%s", code, stderr.String())
			}
			if code == 1 && !strings.Contains(stderr.String(), "Go heap allocation=") {
				t.Fatalf("missing memory sample: %s", stderr.String())
			}
		})
	}
	for _, args := range [][]string{nil, {"--max-files", "0", "--max-relations", "0"}, {"--max-files", "20", "--max-relations", "1000"}} {
		r := runJSON(t, append([]string{"impact", "--repo", repo, "--json"}, args...), "")
		if !r.Complete || len(r.AffectedFiles) == 0 {
			t.Fatalf("retry lost impact: %+v", r)
		}
	}
}
