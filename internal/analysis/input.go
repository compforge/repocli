package analysis

import (
	"context"
	"fmt"

	"github.com/compforge/repocli/internal/git"
)

// InputRequest selects working-tree, index or committed repository material.
// Selection does not imply content capture, hashing, or static analysis.
type InputRequest struct {
	Repository string
	Head       string
	Staged     bool
}

func selectInput(ctx context.Context, req InputRequest) (*git.Repository, string, string, error) {
	if req.Head != "" && req.Staged {
		return nil, "", "", fmt.Errorf("head and staged are mutually exclusive")
	}
	repo, err := git.Open(ctx, req.Repository)
	if err != nil {
		return nil, "", "", err
	}
	input, head := "working_tree", ""
	if req.Head != "" {
		input = "commit"
		head, err = repo.Resolve(ctx, req.Head)
		if err != nil {
			return nil, "", "", err
		}
	} else if req.Staged {
		input = "index"
	}
	return repo, input, head, nil
}
