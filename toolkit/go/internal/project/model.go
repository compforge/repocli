// Package project describes source ownership independently of any execution workflow.
// Identity semantics follow quality-harness's Product/Repository/Component model.
package project

import "github.com/compforge/quality-harness/sdks/go/common"

// Binding separates a component's stable identity from directory layout and
// many-to-many product membership. Neither belongs inside Product's identity.
type Binding struct {
	common.Component
	Manifests    []string         `json:"manifests,omitempty"`
	Root         string           `json:"root"`
	Products     []common.Product `json:"products"`
	PackageTools []PackageTool    `json:"packageTools,omitempty"`
}

type Layout struct {
	Repository *common.Repository `json:"repository"`
	Components []Binding          `json:"components"`
}

// ComponentImpact separates observed impact from analysis completeness.
// Affected=false records no known impact; with Complete=false the result is unknown.
type ComponentImpact struct {
	Component       common.Component `json:"component"`
	Root            string           `json:"root"`
	Snapshot        string           `json:"snapshot"`
	Products        []common.Product `json:"products"`
	PackageTools    []PackageTool    `json:"packageTools,omitempty"`
	Affected        bool             `json:"affected"`
	ChangedFiles    []string         `json:"changedFiles"`
	AffectedFiles   []string         `json:"affectedFiles"`
	SourceFiles     []string         `json:"sourceFiles"`
	TestFiles       []string         `json:"testFiles"`
	Scope           string           `json:"scope"`
	Complete        bool             `json:"complete"`
	FallbackReasons []string         `json:"fallbackReasons,omitempty"`
}
