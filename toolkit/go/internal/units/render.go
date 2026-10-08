package units

import (
	"fmt"
	"strings"
)

func countChanges(hunks []Hunk) (insertions, deletions int64) {
	for _, hunk := range hunks {
		for _, line := range hunk.Lines {
			switch line.Type {
			case HunkAdded:
				insertions++
			case HunkDeleted:
				deletions++
			}
		}
	}
	return insertions, deletions
}

func diffHeader(rawDiff string) string {
	if strings.HasPrefix(rawDiff, "@@") {
		return ""
	}
	if i := strings.Index(rawDiff, "\n@@"); i >= 0 {
		return rawDiff[:i+1]
	}
	return rawDiff
}

func renderHunks(hunks []Hunk) string {
	var rendered strings.Builder
	for _, hunk := range hunks {
		fmt.Fprintf(&rendered, "@@ -%d,%d +%d,%d @@\n", hunk.OldStart, hunk.OldCount, hunk.NewStart, hunk.NewCount)
		for _, line := range hunk.Lines {
			switch line.Type {
			case HunkAdded:
				rendered.WriteString("+" + line.Content + "\n")
			case HunkDeleted:
				rendered.WriteString("-" + line.Content + "\n")
			default:
				rendered.WriteString(" " + line.Content + "\n")
			}
		}
	}
	return rendered.String()
}
