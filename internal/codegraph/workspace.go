package codegraph

import (
	"encoding/json"
	"path"
	"sort"
	"strings"
)

// Resolve explicit source entrypoints from captured manifests. Conditional
// export alternatives are inferred because no runtime condition set is supplied.
func (r *resolver) workspaceImport(root, spec, pkg string) ([]string, *importGuess) {
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
					return nil, nil
				}
			} else if key != "." {
				return nil, nil
			}
		} else if key != "." {
			return nil, nil
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
			return nil, nil
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
	var found []string
	inferred := len(targets) > 1
	for _, target := range targets {
		if path.IsAbs(target) || strings.Contains(target, "*") {
			continue
		}
		name := path.Join(root, target)
		if name == ".." || strings.HasPrefix(name, "../") || (root != "." && !strings.HasPrefix(name, root+"/")) {
			continue
		}
		// Reuse relative extension/index resolution, including JS-to-TS source mapping.
		relative := "./" + strings.TrimPrefix(name, root+"/")
		if root == "." {
			relative = "./" + name
		}
		resolved, issue := r.jsImport(path.Join(root, "package.json"), relative)
		if issue != nil {
			inferred = true
			resolved = issue.Targets
		}
		found = append(found, resolved...)
	}
	if inferred && len(found) > 0 {
		return nil, importCandidates("workspace_export_candidates", found)
	}
	return unique(found), nil
}
