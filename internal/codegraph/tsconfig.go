package codegraph

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
)

type tsConfig struct {
	Extends json.RawMessage            `json:"extends"`
	Options map[string]json.RawMessage `json:"compilerOptions"`
}

// Resolve local inheritance from captured bytes, never from node_modules or
// host filesystem configuration. Unsupported resolution options remain gaps.
func (r *resolver) checkTSConfigs() {
	cache := map[string]map[string]json.RawMessage{}
	visiting := map[string]bool{}
	var read func(string) (map[string]json.RawMessage, error)
	read = func(name string) (map[string]json.RawMessage, error) {
		if options, ok := cache[name]; ok {
			return options, nil
		}
		if visiting[name] {
			return nil, fmt.Errorf("cyclic TypeScript extends: %s", name)
		}
		data, ok := r.files[name]
		if !ok {
			return nil, fmt.Errorf("missing TypeScript config: %s", name)
		}
		var config tsConfig
		if err := json.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("invalid TypeScript config: %s", name)
		}
		visiting[name] = true
		defer delete(visiting, name)
		options := map[string]json.RawMessage{}
		var parents []string
		if len(config.Extends) > 0 {
			var single string
			if json.Unmarshal(config.Extends, &single) == nil {
				parents = []string{single}
			} else if json.Unmarshal(config.Extends, &parents) != nil {
				return nil, fmt.Errorf("unsupported TypeScript extends: %s", name)
			}
		}
		for _, parent := range parents {
			if !strings.HasPrefix(parent, "./") && !strings.HasPrefix(parent, "../") {
				return nil, fmt.Errorf("package TypeScript extends is not resolved: %s", parent)
			}
			target := path.Join(path.Dir(name), parent)
			if target == ".." || strings.HasPrefix(target, "../") {
				return nil, fmt.Errorf("TypeScript extends escapes repository: %s", parent)
			}
			if _, ok := r.files[target]; !ok && !strings.HasSuffix(target, ".json") {
				target += ".json"
			}
			r.configDependencies[name] = append(r.configDependencies[name], target)
			inherited, err := read(target)
			if err != nil {
				return nil, err
			}
			for k, v := range inherited {
				options[k] = v
			}
		}
		for k, v := range config.Options {
			options[k] = v
		}
		cache[name] = options
		return options, nil
	}
	var names []string
	for name := range r.files {
		if strings.HasPrefix(path.Base(name), "tsconfig") && strings.HasSuffix(name, ".json") && !ignoredDependency(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		options, err := read(name)
		if err == nil {
			for _, key := range []string{"paths", "baseUrl", "rootDirs", "moduleSuffixes", "customConditions"} {
				if _, ok := options[key]; ok {
					err = fmt.Errorf("custom TypeScript resolution is not supported: %s", key)
					break
				}
			}
		}
		if err != nil {
			r.configIssues = append(r.configIssues, Issue{Path: name, Message: err.Error(), Configuration: true})
		}
	}
}
