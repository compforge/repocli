package impact

import (
	"encoding/json"
	"path"
	"reflect"
	"strings"
)

// Classification describes the supported dependency model, not runtime safety.
// Explicit imports are still queried even when an asset is documentation.
func metadataChange(name string, before, after []byte) (Uncertainty, bool) {
	observation := Uncertainty{Path: name, Scope: "file", Disposition: "metadata_only"}
	base := strings.ToLower(path.Base(name))
	if strings.HasPrefix(base, "readme") && (path.Ext(base) == ".md" || path.Ext(base) == ".rst") {
		observation.Reason = "documentation_change"
		observation.Message = "documentation change; no implicit source dependency is modeled"
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
