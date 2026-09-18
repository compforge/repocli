package codegraph

import (
	"encoding/json"
	"path"
	"sort"
	"strings"
)

// Resolve explicit source entrypoints from captured manifests. Conditional
// exports must agree on a target because this CLI has no runtime condition set.
func (r *resolver) workspaceImport(root, spec, pkg string) ([]string, *Issue) {
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
					return nil, importIssue("unresolved_import", "workspace export is not resolved: "+spec, nil)
				}
			} else if key != "." {
				return nil, importIssue("unresolved_import", "workspace export is not resolved: "+spec, nil)
			}
		} else if key != "." {
			return nil, importIssue("unresolved_import", "workspace export is not resolved: "+spec, nil)
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
			return nil, importIssue("unsupported_resolution", "unsupported workspace exports: "+spec, nil)
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
		return nil, importIssue("ambiguous_import", "ambiguous workspace export conditions: "+spec, nil)
	}
	var found []string
	for _, target := range targets {
		if path.IsAbs(target) || strings.Contains(target, "*") {
			return nil, importIssue("unsupported_resolution", "unsupported workspace export target: "+spec, nil)
		}
		name := path.Join(root, target)
		if name == ".." || strings.HasPrefix(name, "../") || (root != "." && !strings.HasPrefix(name, root+"/")) {
			return nil, importIssue("unsupported_resolution", "workspace export escapes package: "+spec, nil)
		}
		// Reuse relative extension/index resolution, including JS-to-TS source mapping.
		relative := "./" + strings.TrimPrefix(name, root+"/")
		if root == "." {
			relative = "./" + name
		}
		resolved, issue := r.jsImport(path.Join(root, "package.json"), relative)
		if issue != nil {
			return nil, importIssue("unresolved_import", "workspace export target is not captured: "+spec+" -> "+target, nil)
		}
		found = append(found, resolved...)
	}
	return unique(found), nil
}
