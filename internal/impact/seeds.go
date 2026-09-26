package impact

import (
	"sort"

	"github.com/compforge/repocli/internal/codegraph"
	"github.com/compforge/repocli/internal/diff"
)

// Seed explains why a change enters the graph at this granularity. Confidence
// on a resulting path concerns its edges, not the precision of this choice.
type Seed struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	Version     string `json:"version"`
	Granularity string `json:"granularity"`
	Basis       string `json:"basis"`
}

func selectSeeds(c diff.Change, source codegraph.Source, before bool) []Seed {
	name, version := c.Path, "after"
	if before {
		version = "before"
		if c.OldPath != "" {
			name = c.OldPath
		}
	}
	file := Seed{ID: name, Path: name, Version: version, Granularity: "file", Basis: "file_change"}
	if source.Language == "go" {
		file.Granularity, file.Basis = "package", "go_package"
		return []Seed{file}
	}
	if c.Status != "modified" || IsTest(name) || len(c.Hunks) == 0 {
		return []Seed{file}
	}
	for _, issue := range source.Diagnostics {
		// Missing declarations cannot justify narrow seeds. The diagnostic's
		// location bounds this decision, even if the upstream only knows a file.
		if issue.Subject != "declarations" && issue.Subject != "document" {
			continue
		}
		for _, h := range c.Hunks {
			r := h.New
			if before {
				r = h.Old
			}
			if r.Count > 0 && (issue.Location.EndLine == 0 || r.Start <= issue.Location.EndLine && r.Start+r.Count-1 >= issue.Location.Line) {
				file.Basis = "declaration_gap"
				return []Seed{file}
			}
		}
	}
	selected := map[string]Seed{}
	for _, h := range c.Hunks {
		r := h.New
		if before {
			r = h.Old
		}
		if r.Count == 0 {
			continue
		}
		covered := false
		for _, symbol := range source.Symbols {
			if r.Start < symbol.StartLine || r.Start+r.Count-1 > symbol.EndLine {
				continue
			}
			covered = true
			id := codegraph.SymbolID(name, symbol.QualifiedName)
			selected[id] = Seed{ID: id, Path: name, Version: version, Granularity: "symbol", Basis: "changed_declaration"}
		}
		if !covered {
			file.Basis = "module_change"
			return []Seed{file}
		}
	}
	if len(selected) == 0 {
		return nil
	} // An insertion anchor is not a change in the other version.
	module := codegraph.ModuleID(name)
	selected[module] = Seed{ID: module, Path: name, Version: version, Granularity: "module", Basis: "whole_module_import"}
	seeds := make([]Seed, 0, len(selected))
	for _, seed := range selected {
		seeds = append(seeds, seed)
	}
	sort.Slice(seeds, func(i, j int) bool { return seeds[i].ID < seeds[j].ID })
	return seeds
}
