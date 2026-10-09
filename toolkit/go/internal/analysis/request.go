package analysis

import (
	"fmt"

	"github.com/compforge/repocli/toolkit/go/internal/impact"
)

// Validate checks comparison options without reading repository contents.
// The CLI uses this for usage errors; Analyze repeats it for direct library calls.
func (req Request) Validate() error {
	if req.MaxFiles < 0 || req.MaxRelations < 0 {
		return fmt.Errorf("MaxFiles and MaxRelations must not be negative")
	}
	if req.EmptyBase && req.Base != "" {
		return fmt.Errorf("base and empty base are mutually exclusive")
	}
	if req.Head != "" && req.Staged || req.PatchFile != "" && (req.Head != "" || req.Staged) {
		return fmt.Errorf("head, staged and patch are mutually exclusive")
	}
	if req.PatchFile == "-" && req.Stdin == nil {
		return fmt.Errorf("patch input requires a reader")
	}
	_, err := impact.ValidateDirs(req.TestDirs)
	return err
}
