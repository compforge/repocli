package git_test

import (
	"context"
	"strings"
	"testing"

	"github.com/compforge/repocli/toolkit/go/internal/git"
)

func TestCaptureFileBudgetAppliesToEveryComparisonInput(t *testing.T) {
	repo, err := git.Open(context.Background(), fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{1, 4} {
		repo.MaxFiles = limit
		for _, input := range []string{"commit", "index", "working"} {
			var err error
			switch input {
			case "commit":
				_, err = repo.Base(context.Background(), "HEAD")
			case "index":
				_, err = repo.Staged(context.Background())
			case "working":
				_, _, err = repo.Working(context.Background())
			}
			if limit == 4 {
				if err != nil {
					t.Fatalf("%s: increased budget failed: %v", input, err)
				}
				continue
			}
			if err == nil || !strings.Contains(err.Error(), "files=4 limit=1") || !strings.Contains(err.Error(), "Go heap allocation=") {
				t.Fatalf("%s: missing file budget evidence: %v", input, err)
			}
		}
	}
}

func TestCaptureDefaultFileBudgetCanBeExpanded(t *testing.T) {
	repo := &git.Repository{}
	if err := repo.CheckFileCount(10000); err != nil {
		t.Fatal(err)
	}
	if err := repo.CheckFileCount(10001); err == nil {
		t.Fatal("default file budget not enforced")
	}
	repo.MaxFiles = 20000
	if err := repo.CheckFileCount(10001); err != nil {
		t.Fatal(err)
	}
}
