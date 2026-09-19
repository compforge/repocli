package impact

import (
	"fmt"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/compforge/repocli/internal/codegraph"
	"github.com/compforge/repocli/internal/diff"
)

// Defaults preserve language conventions; directories constrain search separately.
var defaultTestPatterns = map[string][]string{
	"go":         {"**/*_test.go"},
	"python":     {"**/test_*.py", "**/*_test.py"},
	"typescript": {"**/*.test.*", "**/*.spec.*", "**/__tests__/**"},
	"tsx":        {"**/*.test.*", "**/*.spec.*", "**/__tests__/**"},
	"javascript": {"**/*.test.*", "**/*.spec.*", "**/__tests__/**"},
}

// ValidateTestPatterns accepts repository-relative, slash-separated globs. An
// empty list selects language defaults; explicit patterns replace those defaults.
func ValidateTestPatterns(patterns []string) ([]string, error) {
	out := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.TrimPrefix(pattern, "./")
		if !diff.ValidPath(pattern) || !doublestar.ValidatePattern(pattern) {
			return nil, fmt.Errorf("invalid test pattern %q: expected a repository-relative glob", pattern)
		}
		out = append(out, pattern)
	}
	return unique(out), nil
}

func isTest(name string, patterns []string) bool {
	language := codegraph.Language(name)
	if language == "" {
		return false
	}
	if len(patterns) == 0 {
		patterns = defaultTestPatterns[language]
	}
	for _, pattern := range patterns {
		// Patterns were validated once per request. Match captured paths only;
		// filesystem globbing would read live bytes outside the analyzed snapshot.
		if doublestar.MatchUnvalidated(pattern, name) {
			return true
		}
	}
	return false
}
