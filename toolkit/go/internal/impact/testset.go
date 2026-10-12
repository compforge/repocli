package impact

import (
	"path"
	"strings"

	"github.com/compforge/repocli/toolkit/go/internal/project"
)

// discoverTests finds candidates without rewriting the snapshot catalog.
// Explicit directories replace default discovery, not production source scope.
// +spec=`Test location and Component ownership are independent; an empty Testset is valid`
// +link=repo://docs/diff.md#test-discovery
func discoverTests(files map[string][]byte, layout project.Layout, components componentScope, dirs []string) []string {
	tests := []string{}
	for name := range files {
		if !IsTest(name) || ignoredDependency(name) {
			continue
		}
		if len(dirs) != 0 {
			if within(name, dirs) {
				tests = append(tests, name)
			}
			continue
		}
		if components.contains(layout, name) || path.Dir(name) == "." || repositoryTestDirectory(name) {
			tests = append(tests, name)
		}
	}
	return unique(tests)
}

func repositoryTestDirectory(name string) bool {
	first, _, ok := strings.Cut(name, "/")
	return ok && testDirectory(first)
}

func testDirectory(name string) bool {
	return name == "tests" || name == "test" || name == "__tests__"
}

// testSupport admits helpers on demand inside declared or conventional test
// directories. Searching "." never declares all repository sources as helpers.
func testSupport(name string, dirs []string) bool {
	for _, dir := range dirs {
		if dir != "." && within(name, []string{dir}) {
			return true
		}
	}
	for _, part := range strings.Split(path.Dir(name), "/") {
		if testDirectory(part) {
			return true
		}
	}
	return false
}
