package forge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func githubFixture(number int, state string) string {
	merged := "null"
	if state == "merged" {
		state = "closed"
		merged = `"2026-01-01T00:00:00Z"`
	}
	return fmt.Sprintf(`{"number":%d,"title":"fixture","state":%q,"merged_at":%s,"html_url":"https://github.com/acme/repo/pull/%d","head":{"sha":"abc","ref":"topic","repo":{"full_name":"acme/repo"}},"base":{"ref":"main","repo":{"full_name":"acme/repo"}}}`, number, state, merged, number)
}
func TestGitHubPaginationSourceIdentityAndMergedState(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("credential missing")
		}
		if r.URL.Query().Get("page") == "1" {
			rows := make([]json.RawMessage, 100)
			for i := range rows {
				rows[i] = json.RawMessage(strings.ReplaceAll(githubFixture(i+1, "merged"), `"full_name":"acme/repo"`, `"full_name":"other/repo"`))
			} // Restore target while retaining a different source.
			for i := range rows {
				rows[i] = json.RawMessage(strings.ReplaceAll(string(rows[i]), `"base":{"ref":"main","repo":{"full_name":"other/repo"}}`, `"base":{"ref":"main","repo":{"full_name":"acme/repo"}}`))
			}
			_ = json.NewEncoder(w).Encode(rows)
			return
		}
		fmt.Fprintf(w, "[%s]", githubFixture(101, "merged"))
	}))
	defer server.Close()
	client, err := NewClient(Config{Host: "github.com", Token: "fixture", APIHost: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := client.List(context.Background(), "acme/repo", "merged", "acme/repo", "topic")
	if err != nil || len(rows) != 1 || rows[0].Key.Number != 101 || calls != 2 {
		t.Fatal(rows, err, calls)
	}
}
func TestWriteFailureIsUncertainAndNeverRetried(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(503) }))
	defer server.Close()
	client, _ := NewClient(Config{Host: "github.com", APIHost: server.URL})
	_, err := client.Close(context.Background(), Key{client.Repository("acme/repo"), 1})
	var unknown *OutcomeUnknown
	if !errors.As(err, &unknown) || calls != 1 {
		t.Fatal(err, calls)
	}
}
func TestGitLabForkCreateAndMergeExpectedSHA(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.EscapedPath() == "/projects/upstream%2Frepo":
			fmt.Fprint(w, `{"id":9}`)
		case r.Method == "GET" && r.URL.Path == "/projects/8":
			fmt.Fprint(w, `{"path_with_namespace":"fork/repo"}`)
		case r.Method == "POST":
			if r.URL.EscapedPath() != "/projects/fork%2Frepo/merge_requests" {
				t.Error(r.URL)
			}
			var data map[string]any
			_ = json.NewDecoder(r.Body).Decode(&data)
			if data["target_project_id"] != float64(9) || data["source_branch"] != "topic" {
				t.Error(data)
			}
			fmt.Fprint(w, `{"iid":2,"title":"fixture","state":"opened","web_url":"https://gitlab.com/upstream/repo/-/merge_requests/2","sha":"abc","source_branch":"topic","target_branch":"main","source_project_id":8,"target_project_id":9}`)
		default:
			t.Error(r.Method, r.URL)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	client, _ := NewClient(Config{Host: "gitlab.com", APIHost: server.URL})
	pr, err := client.Create(context.Background(), CreateRequest{SourceRepository: "fork/repo", SourceBranch: "topic", TargetRepository: "upstream/repo", TargetBranch: "main", Title: "fixture"})
	if err != nil || pr.Key.Repository.Path != "upstream/repo" || pr.SourceRepository.Path != "fork/repo" {
		t.Fatal(pr, err)
	}
	if _, err = client.Merge(context.Background(), pr.Key, "", "merge"); err == nil {
		t.Fatal("missing expected SHA accepted")
	}
}
func TestRateLimitCooldownAndMalformedList(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(429)
	}))
	defer server.Close()
	client, _ := NewClient(Config{Host: "github.com", APIHost: server.URL})
	for i := 0; i < 2; i++ {
		_, err := client.List(context.Background(), "acme/repo", "open", "", "")
		var limited *HTTPError
		if !errors.As(err, &limited) || limited.RetryAt.IsZero() {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatal("cooldown ignored", calls)
	}
}

func TestContextCreateReusesOpenPRAndDoesNotTurnMaintenanceFailureIntoWriteFailure(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v", out, err)
		}
	}
	git("init", "-q")
	git("remote", "add", "origin", "https://github.com/acme/repo.git")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null", "commit", "--allow-empty", "-qm", "initial")
	// A malformed maintenance policy must not change a successful remote result.
	if err := os.WriteFile(filepath.Join(root, ".git", "repokit.json"), []byte(`{"worktree":{"max_count":-1}}`), 0600); err != nil {
		t.Fatal(err)
	}
	created := false
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			created = true
			writes++
			fmt.Fprint(w, githubFixture(1, "open"))
			return
		}
		if !created {
			fmt.Fprint(w, "[]")
		} else {
			fmt.Fprintf(w, "[%s]", githubFixture(1, "open"))
		}
	}))
	defer server.Close()
	client, _ := NewClient(Config{Host: "github.com", APIHost: server.URL})
	ctx, err := NewContext(context.Background(), client, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := CreateRequest{SourceBranch: "topic", TargetRepository: "acme/repo", TargetBranch: "main", Title: "fixture"}
	for i := 0; i < 2; i++ {
		result, err := ctx.Ensure(context.Background(), request)
		if err != nil || result.Created != (i == 0) || len(result.Maintenance.Errors) == 0 {
			t.Fatal(result, err)
		}
	}
	if writes != 1 {
		t.Fatal("duplicate creation", writes)
	}
}
