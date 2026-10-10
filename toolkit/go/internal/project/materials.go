package project

import "path"

// NeedsContent defines inspection's read set. Other files contribute only their
// regular-file path: markers and lockfiles need no contents.
// Keep this aligned with Load and package-tool detection so all commands share
// one interpretation of a selected repository version.
func NeedsContent(name string) bool {
	return path.Base(name) == "package.json"
}
