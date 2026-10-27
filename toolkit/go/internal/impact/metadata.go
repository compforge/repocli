package impact

import (
	"encoding/json"
	"path"
	"reflect"
	"slices"

	cg "github.com/compforge/codegraph"
	"github.com/compforge/repocli/toolkit/go/internal/project"
)

// Classification describes the supported dependency model, not runtime safety.
// +spec=Documentation and version tags suppress only implicit dependency gaps; explicit imports remain query seeds.
// +link=component://docs/diff.md
func metadataChange(name string, before, after []byte) (Uncertainty, bool) {
	observation := Uncertainty{Path: name, Scope: "file", Disposition: "metadata_only"}
	tags := project.FileTags(name, "")
	if slices.Contains(tags, cg.DocumentationTag) {
		observation.Reason = "documentation_change"
		observation.Message = "documentation change; no implicit source dependency is modeled"
		return observation, true
	}
	if slices.Contains(tags, cg.VersionTag) {
		observation.Reason = "version_change"
		observation.Message = "version marker change; no implicit source dependency is modeled"
		return observation, true
	}
	if path.Base(name) != "package.json" {
		return Uncertainty{}, false
	}
	var old, new map[string]any
	if json.Unmarshal(before, &old) != nil || json.Unmarshal(after, &new) != nil || old == nil || new == nil {
		return Uncertainty{}, false
	}
	// Version, names, exports, scripts and unknown keys may change execution or
	// workspace resolution. Only explicitly descriptive fields are excluded.
	changed := false
	for _, key := range []string{"description", "keywords", "author", "contributors", "license", "homepage", "bugs", "repository"} {
		a, aok := old[key]
		b, bok := new[key]
		changed = changed || aok != bok || !reflect.DeepEqual(a, b)
		delete(old, key)
		delete(new, key)
	}
	if !changed || !reflect.DeepEqual(old, new) {
		return Uncertainty{}, false
	}
	observation.Reason = "descriptive_manifest_change"
	observation.Message = "only descriptive package manifest fields changed"
	return observation, true
}
