package git_test

import (
	"context"
	"strings"
	"testing"

	repogit "github.com/compforge/repocli/toolkit/go/internal/git"
	"github.com/compforge/repocli/toolkit/go/internal/project"
)

func TestCatalogKeepsOnlyMetadataContents(t *testing.T) {
	dir := fixture(t)
	put(t, dir, "large.go", strings.Repeat("x", 3<<20))
	commit := strings.TrimSpace(gitCommand(t, dir, "rev-parse", "HEAD"))
	gitCommand(t, dir, "update-index", "--add", "--cacheinfo", "160000,"+commit+",child")
	repo, err := repogit.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := repo.Catalog(context.Background(), "", false, project.NeedsContent)
	if err != nil {
		t.Fatal(err)
	}
	if content, ok := catalog["large.go"]; !ok || content != nil {
		t.Fatal("large source content entered inspection")
	}
	if _, ok := catalog["child"]; ok {
		t.Fatal("gitlink entered component catalog")
	}
}
