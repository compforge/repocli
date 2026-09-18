package impact

import (
	"encoding/json"
	"path"
	"sort"
	"strings"
)

// Resolve explicit source entrypoints from captured manifests. Conditional
// exports must agree on a target because this CLI has no runtime condition set.
func (r *resolver) workspaceImport(root, spec, pkg string) ([]string, string) {
	m := r.packages[root]
	key := "."
	if spec != pkg {
		key = "./" + strings.TrimPrefix(spec, pkg+"/")
	}
	var targets []string
	if len(m.Exports) > 0 {
		value := m.Exports
		var entries map[string]json.RawMessage
		if json.Unmarshal(value, &entries) == nil {
			subpaths := false
			for k := range entries {
				if strings.HasPrefix(k, ".") {
					subpaths = true
				}
			}
			if subpaths {
				var ok bool
				value, ok = entries[key]
				if !ok {
					return nil, "workspace export is not resolved: " + spec
				}
			} else if key != "." {
				return nil, "workspace export is not resolved: " + spec
			}
		} else if key != "." {
			return nil, "workspace export is not resolved: " + spec
		}
		var collect func(json.RawMessage) bool
		collect = func(raw json.RawMessage) bool {
			var target string
			if json.Unmarshal(raw, &target) == nil {
				targets = append(targets, target)
				return true
			}
			var conditions map[string]json.RawMessage
			if json.Unmarshal(raw, &conditions) != nil || len(conditions) == 0 {
				return false
			}
			var keys []string
			for k := range conditions {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if strings.HasPrefix(k, ".") || !collect(conditions[k]) {
					return false
				}
			}
			return true
		}
		if !collect(value) {
			return nil, "unsupported workspace exports: " + spec
		}
	} else if key == "." {
		if m.Main != "" {
			targets = append(targets, m.Main)
		}
		if m.Module != "" {
			targets = append(targets, m.Module)
		}
		if len(targets) == 0 {
			targets = []string{"./index"}
		}
	} else {
		targets = []string{key}
	}
	targets = unique(targets)
	if len(targets) != 1 {
		return nil, "ambiguous workspace export conditions: " + spec
	}
	var found []string
	for _, target := range targets {
		if path.IsAbs(target) || strings.Contains(target, "*") {
			return nil, "unsupported workspace export target: " + spec
		}
		name := path.Join(root, target)
		if name == ".." || strings.HasPrefix(name, "../") || (root != "." && !strings.HasPrefix(name, root+"/")) {
			return nil, "workspace export escapes package: " + spec
		}
		// Reuse relative extension/index resolution, including JS-to-TS source mapping.
		relative := "./" + strings.TrimPrefix(name, root+"/")
		if root == "." {
			relative = "./" + name
		}
		resolved, issue := r.jsImport(path.Join(root, "package.json"), relative)
		if issue != "" {
			return nil, "workspace export target is not captured: " + spec + " -> " + target
		}
		found = append(found, resolved...)
	}
	return unique(found), ""
}
