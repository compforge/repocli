package analysis

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	cg "github.com/compforge/codegraph"
)

type pathTag struct {
	name    cg.Tag
	pattern *regexp.Regexp
}
type pathTags []pathTag

// Diff classifies captured paths without parsing or constructing a graph. The
// vocabulary and builtin patterns come from CodeGraph; its graph-only matcher
// is not public. Keep matching equivalent for later graph consumers.
// +spec=Path tags never discard changes, and each comparison side uses its own path.
func compilePathTags(rules []cg.TagRule) (pathTags, error) {
	if rules == nil {
		rules = cg.BuiltinTagRules()
	}
	tags := make(pathTags, 0, len(rules))
	for i, rule := range rules {
		if strings.TrimSpace(string(rule.Name)) == "" {
			return nil, fmt.Errorf("tag rule %d: name is required", i)
		}
		pattern, err := regexp.Compile(rule.Pattern)
		if err != nil {
			return nil, fmt.Errorf("tag rule %d (%q): %w", i, rule.Name, err)
		}
		tags = append(tags, pathTag{rule.Name, pattern})
	}
	return tags, nil
}

func (tags pathTags) match(path string) []cg.Tag {
	var out []cg.Tag
	for _, tag := range tags {
		if tag.pattern.MatchString(path) {
			out = append(out, tag.name)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}
