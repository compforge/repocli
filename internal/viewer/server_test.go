package viewer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	shared "github.com/compforge/codegraph"
	"github.com/compforge/repocli/internal/analysis"
)

func repository(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("init: %v %s", err, out)
	}
	files := map[string]string{
		".gitignore":    "ignored.ts\n",
		"ignored.ts":    "export function secret() {}",
		"lib.ts":        "export interface Contract { run(): void; }\nexport class Base { run() {} }\n",
		"app.ts":        "import { Base, Contract } from './lib';\nexport class Child extends Base implements Contract { run() {} }\nexport function work() {}\nexport function entry() { work(); return work; }\n",
		"notes.unknown": "Captured text without a grammar.\n",
	}
	for name, content := range files {
		put(t, dir, name, content)
	}
	return dir
}

func put(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func loader(dir string) Loader {
	return func(ctx context.Context) (*analysis.GraphSnapshot, error) {
		return analysis.CaptureGraph(ctx, analysis.SnapshotRequest{Repository: dir}, 100)
	}
}

func request(h http.Handler, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://localhost"+path, nil)
	req.Header.Set("X-Repocli-View", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// +case=`The viewer preserves shared graph identities, relation evidence and local diagnostics`
func TestGraphAndSourceAPI(t *testing.T) {
	dir := repository(t)
	graph, err := loader(dir)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s := &server{current: graph, load: loader(dir)}
	h := s.handler()
	w := request(h, "GET", "/api/graph")
	var got analysis.GraphSnapshot
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	kinds := map[shared.RelationKind]bool{}
	for _, r := range got.Relations {
		kinds[r.Kind] = true
		if r.ID == "" || r.Basis == "" || r.Confidence == "" || r.Location.Path == "" {
			t.Fatalf("lost evidence: %+v", r)
		}
	}
	for _, kind := range []shared.RelationKind{shared.Contains, shared.Imports, shared.Calls, shared.References, shared.Extends, shared.Implements} {
		if !kinds[kind] {
			t.Fatalf("missing %s: %+v", kind, got.Relations)
		}
	}
	hasDocument, hasDiagnostic := false, false
	for _, n := range got.Nodes {
		if n.Location.Path == "ignored.ts" {
			t.Fatal("ignored source was included")
		}
		if n.Kind == shared.DocumentKind && n.Location.Path == "notes.unknown" {
			hasDocument = true
		}
	}
	for _, d := range got.Diagnostics {
		if d.Code == "unsupported_language" && d.Location.Path == "notes.unknown" {
			hasDiagnostic = true
		}
	}
	if !hasDocument || !hasDiagnostic {
		t.Fatal("missing grammarless document or diagnosis")
	}
	if strings.Contains(w.Body.String(), "Captured text without") {
		t.Fatal("graph response leaked source body")
	}
	q := "/api/source?" + url.Values{"snapshot": {got.Snapshot.Snapshot}, "path": {"app.ts"}}.Encode()
	put(t, dir, "app.ts", "export function changed() {}\n")
	if w = request(h, "GET", q); w.Code != 200 || !strings.Contains(w.Body.String(), "entry()") || strings.Contains(w.Body.String(), "changed()") {
		t.Fatal("source drifted from graph", w.Body.String())
	}
	if w = request(h, "POST", "/api/refresh"); w.Code != 200 || !strings.Contains(w.Body.String(), "changed") {
		t.Fatal("refresh failed", w.Body.String())
	}
	if w = request(h, "GET", q); w.Code != http.StatusConflict {
		t.Fatal("stale source request accepted", w.Code)
	}
	for _, path := range []string{"../outside", "ignored.ts", "/etc/passwd"} {
		q = "/api/source?" + url.Values{"snapshot": {s.snapshot().Snapshot.Snapshot}, "path": {path}}.Encode()
		if w = request(h, "GET", q); w.Code != 404 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	for _, asset := range []string{"/", "/app.js", "/style.css", "/vendor/cytoscape.min.js", "/vendor/LICENSE.cytoscape"} {
		if w = request(h, "GET", asset); w.Code != 200 || w.Body.Len() == 0 {
			t.Fatal(asset, w.Code)
		}
	}
}

// +case=`Refresh failure or overlap never replaces the published graph`
func TestRefreshFailureAndConcurrency(t *testing.T) {
	original := &analysis.GraphSnapshot{Snapshot: analysis.SnapshotReport{Snapshot: "old"}}
	entered, release := make(chan struct{}), make(chan struct{})
	s := &server{current: original, load: func(context.Context) (*analysis.GraphSnapshot, error) {
		close(entered)
		<-release
		return nil, errors.New("capture failed")
	}}
	h := s.handler()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- request(h, "POST", "/api/refresh") }()
	<-entered
	if w := request(h, "POST", "/api/refresh"); w.Code != 409 {
		t.Fatal(w.Code)
	}
	if w := request(h, "GET", "/api/graph"); w.Code != 200 || !strings.Contains(w.Body.String(), "old") {
		t.Fatal(w.Body.String())
	}
	close(release)
	if w := <-done; w.Code != 500 {
		t.Fatal(w.Code)
	}
	if s.snapshot() != original {
		t.Fatal("failed refresh changed graph")
	}
}

func TestLocalBoundaryAndBuildLimit(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:80", ":5484", "example.com:5484", "127.0.0.1:no", "127.0.0.1:65536"} {
		if ValidateAddress(addr) == nil {
			t.Fatal("accepted", addr)
		}
	}
	for _, addr := range []string{"127.0.0.1:0", "localhost:5484", "[::1]:5484"} {
		if err := ValidateAddress(addr); err != nil {
			t.Fatal(err)
		}
	}
	s := &server{current: &analysis.GraphSnapshot{}}
	h := s.handler()
	for _, tc := range []struct{ host, origin, method string }{
		{"attacker.invalid", "", "GET"}, {"localhost", "https://attacker.invalid", "GET"}, {"localhost", "", "POST"},
	} {
		req := httptest.NewRequest(tc.method, "http://"+tc.host+"/api/refresh", nil)
		req.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 403 {
			t.Fatal(tc, w.Code)
		}
	}
	graph, err := analysis.CaptureGraph(context.Background(), analysis.SnapshotRequest{Repository: repository(t)}, 1)
	if err != nil {
		t.Fatal(err)
	}
	limitRecorded := false
	for _, d := range graph.Diagnostics {
		if d.Code == "document_limit" && d.Subject == shared.DocumentSubject {
			limitRecorded = true
		}
	}
	if graph.Documents != 1 || !graph.Snapshot.Complete || !limitRecorded {
		t.Fatalf("graph limit must not change capture completeness: %+v / %+v", graph.Snapshot, graph.Diagnostics)
	}
}

type readyWriter struct {
	once  sync.Once
	ready chan struct{}
}

func (w *readyWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "Code graph:") {
		w.once.Do(func() { close(w.ready) })
	}
	return len(p), nil
}

func TestServerShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writer := &readyWriter{ready: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, "127.0.0.1:0", func(context.Context) (*analysis.GraphSnapshot, error) { return &analysis.GraphSnapshot{}, nil }, writer)
	}()
	select {
	case <-writer.ready:
	case err := <-done:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop")
	}
}
