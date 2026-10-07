package analysis

import (
	"context"
	"path"
	"reflect"
	"sort"

	"github.com/compforge/repocli/toolkit/go/internal/git"
	"github.com/compforge/repocli/toolkit/go/internal/project"
)

// Directory is implied by entries; the repository root is '.'. Git does not track empty directories.
type Directory struct {
	Path string `json:"path"`
}

// Manifest is project metadata, independently of Component discovery policy.
type Manifest struct {
	Path      string `json:"path"`
	Ecosystem string `json:"ecosystem"`
}

// File separates Git entry kind from the semantic role of regular files.
type File struct {
	git.Entry
	Role     string    `json:"role,omitempty"`
	Manifest *Manifest `json:"manifest,omitempty"`
}
type TreeReport struct {
	SchemaVersion int          `json:"schemaVersion"`
	Checkout      string       `json:"checkout"`
	Input         string       `json:"input"`
	Head          string       `json:"head,omitempty"`
	Directories   []Directory  `json:"directories"`
	Files         []File       `json:"files"`
	Complete      bool         `json:"complete"`
	Diagnostics   []Diagnostic `json:"diagnostics"`
}

// Tree observes versioned entries independently of Component configuration or source analysis.
func Tree(ctx context.Context, req InputRequest) (TreeReport, error) {
	repo, input, head, err := selectInput(ctx, req)
	if err != nil {
		return TreeReport{}, err
	}
	entries, err := repo.Entries(ctx, head, req.Staged)
	if err != nil {
		return TreeReport{}, err
	}
	report := TreeReport{SchemaVersion: 1, Checkout: repo.Root, Input: input, Head: head, Files: []File{}, Directories: []Directory{}, Complete: true, Diagnostics: []Diagnostic{}}
	dirs := map[string]bool{".": true}
	for _, entry := range entries {
		file := File{Entry: entry}
		if entry.Kind == "regular" {
			if ecosystem := project.ManifestEcosystem(entry.Path); ecosystem != "" {
				file.Role = "manifest"
				file.Manifest = &Manifest{Path: entry.Path, Ecosystem: ecosystem}
			} else {
				file.Role = project.FileRole(entry.Path)
			}
		}
		report.Files = append(report.Files, file)
		for dir := path.Dir(entry.Path); dir != "."; dir = path.Dir(dir) {
			dirs[dir] = true
		}
	}
	for dir := range dirs {
		report.Directories = append(report.Directories, Directory{Path: dir})
	}
	sort.Slice(report.Directories, func(i, j int) bool { return report.Directories[i].Path < report.Directories[j].Path })
	if head == "" {
		latest, err := repo.Entries(ctx, head, req.Staged)
		if err != nil {
			return TreeReport{}, err
		}
		if !reflect.DeepEqual(entries, latest) {
			report.Complete = false
			report.Diagnostics = append(report.Diagnostics, Diagnostic{Code: "tree_changed", Message: "repository entries changed during observation"})
		}
	}
	return report, nil
}
