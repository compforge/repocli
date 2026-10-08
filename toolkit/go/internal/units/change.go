package units

import cg "github.com/compforge/codegraph"

// Change is a file patch and the captured before/after source used to interpret it.
type Change struct {
	// Tags classify Path(), even when content is binary or unavailable.
	// They do not filter changes or affect Unit identity.
	Tags              []cg.Tag `json:"tags,omitempty"`
	OldPath           string   `json:"old_path"`
	NewPath           string   `json:"new_path"`
	Diff              string   `json:"diff"`
	OldFileContent    string   `json:"old_file_content,omitempty"`
	OldContentKnown   bool     `json:"old_content_known,omitempty"`
	BeforeRef         string   `json:"before_ref,omitempty"`
	AfterRef          string   `json:"after_ref,omitempty"`
	NewContentMissing bool     `json:"new_content_missing,omitempty"`
	NewFileContent    string   `json:"new_file_content"`
	IsBinary          bool     `json:"is_binary"`
	IsDeleted         bool     `json:"is_deleted"`
	IsNew             bool     `json:"is_new"`
	IsRenamed         bool     `json:"is_renamed"`
	Insertions        int64    `json:"insertions"`
	Deletions         int64    `json:"deletions"`
}

// Path is the target's review identity, including files removed from the new tree.
func (d Change) Path() string {
	if d.IsDeleted || d.NewPath == "/dev/null" {
		return d.OldPath
	}
	return d.NewPath
}
