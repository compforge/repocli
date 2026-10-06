// Package viewer serves a read-only code graph and its captured source bytes.
package viewer

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/compforge/repocli"
)

//go:embed static
var assets embed.FS

type Loader func(context.Context) (*repocli.GraphSnapshot, error)

type server struct {
	mu      sync.RWMutex
	refresh sync.Mutex
	current *repocli.GraphSnapshot
	load    Loader
}

func ValidateAddress(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid --addr: %w", err)
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("--addr must use localhost or a loopback IP")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 0 || n > 65535 {
		return fmt.Errorf("--addr port must be between 0 and 65535")
	}
	return nil
}

// Run owns the listener until cancellation. Build deadlines belong to Loader,
// so the ordinary CLI timeout never expires a healthy browsing session.
func Run(ctx context.Context, addr string, load Loader, out io.Writer) error {
	if err := ValidateAddress(addr); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	if _, err := fmt.Fprintln(out, "Building code graph…"); err != nil {
		return err
	}
	initial, err := load(ctx)
	if err != nil {
		return err
	}
	s := &server{current: initial, load: load}
	srv := &http.Server{Handler: s.handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second,
		BaseContext: func(net.Listener) context.Context { return ctx }}
	if _, err := fmt.Fprintf(out, "Code graph: http://%s\n%d documents · %d nodes · %d relations · %d diagnostics\nPress Ctrl+C to stop.\n",
		listener.Addr(), initial.Documents, len(initial.Nodes), len(initial.Relations), len(initial.Diagnostics)+len(initial.Snapshot.Diagnostics)); err != nil {
		return err
	}
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := srv.Shutdown(shutdown); err != nil {
				_ = srv.Close()
			}
		case <-stopped:
		}
	}()
	err = srv.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *server) snapshot() *repocli.GraphSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	static, err := fs.Sub(assets, "static")
	if err != nil {
		panic(err)
	} // Embedded directory is a build-time invariant.
	mux.Handle("GET /", http.FileServer(http.FS(static)))
	mux.HandleFunc("GET /api/graph", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.snapshot()) })
	mux.HandleFunc("GET /api/source", s.source)
	mux.HandleFunc("POST /api/refresh", s.reload)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			http.Error(w, "local host required", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			http.Error(w, "same origin required", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; object-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// +spec=`Refreshing publishes a whole new snapshot; failed refreshes retain the old graph`
func (s *server) reload(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Repocli-View") != "1" {
		http.Error(w, "viewer request required", http.StatusForbidden)
		return
	}
	if !s.refresh.TryLock() {
		http.Error(w, "refresh already running", http.StatusConflict)
		return
	}
	defer s.refresh.Unlock()
	next, err := s.load(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	s.current = next
	s.mu.Unlock()
	writeJSON(w, next)
}

func (s *server) source(w http.ResponseWriter, r *http.Request) {
	snapshot := s.snapshot()
	if r.URL.Query().Get("snapshot") != snapshot.Snapshot.Snapshot {
		http.Error(w, "snapshot changed; reload the graph", http.StatusConflict)
		return
	}
	path := r.URL.Query().Get("path")
	data, ok := snapshot.Sources[path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	lines := strings.Split(string(data), "\n")
	from := 1
	if value := r.URL.Query().Get("from"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > len(lines) {
			http.Error(w, "invalid source line", http.StatusBadRequest)
			return
		}
		from = n
	}
	to := min(len(lines), from+399)
	writeJSON(w, struct {
		Path  string   `json:"path"`
		From  int      `json:"from"`
		Lines []string `json:"lines"`
		Total int      `json:"total"`
	}{path, from, lines[from-1 : to], len(lines)})
}
