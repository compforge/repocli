package repocli

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var version string

// Version returns the toolkit version embedded from the repository VERSION file.
func Version() string { return strings.TrimSpace(version) }
