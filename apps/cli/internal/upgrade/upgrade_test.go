package upgrade

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func archiveBytes(t *testing.T, name string, kind byte) []byte {
	return archiveContent(t, name, kind, []byte("new executable"))
}

func archiveContent(t *testing.T, name string, kind byte, content []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tarball := tar.NewWriter(gz)
	h := &tar.Header{Name: name, Mode: 0755, Size: int64(len(content)), Typeflag: kind}
	if kind == tar.TypeSymlink {
		h.Size = 0
		h.Linkname = "elsewhere"
	}
	if err := tarball.WriteHeader(h); err != nil {
		t.Fatal(err)
	}
	if h.Size > 0 {
		if _, err := tarball.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarball.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func releaseServer(t *testing.T, archive []byte, checksum string) (*Client, *int) {
	t.Helper()
	downloads := new(int)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.18.0", "assets": []map[string]string{
				{"name": "repocli_v0.18.0_darwin_arm64.tar.gz", "browser_download_url": server.URL + "/archive"},
				{"name": "checksums.txt", "browser_download_url": server.URL + "/checksums"},
			}})
		case "/archive":
			*downloads++
			w.Write(archive)
		case "/checksums":
			if checksum == "" {
				checksum = fmt.Sprintf("%x", sha256.Sum256(archive))
			}
			fmt.Fprintf(w, "%s  repocli_v0.18.0_darwin_arm64.tar.gz\n", checksum)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return &Client{http: server.Client(), url: server.URL + "/latest", os: "darwin", arch: "arm64"}, downloads
}

func TestCheckVersionAndPlatform(t *testing.T) {
	client, downloads := releaseServer(t, nil, "")
	for _, tc := range []struct {
		current string
		update  bool
	}{
		{"0.17.0", true}, {"v0.18.0", false}, {"0.19.0", false}, {"0.18.0-rc.1", true},
	} {
		result, err := client.Check(context.Background(), tc.current)
		if err != nil || result.UpdateAvailable != tc.update || result.LatestVersion != "0.18.0" {
			t.Fatalf("%s: %+v %v", tc.current, result, err)
		}
	}
	if *downloads != 0 {
		t.Fatal("check downloaded an archive")
	}
	if _, err := client.Check(context.Background(), "dev"); err == nil {
		t.Fatal("accepted unknown development version")
	}
	client.arch = "arm"
	if _, err := client.Check(context.Background(), "0.17.0"); err == nil {
		t.Fatal("arm selected arm64 asset")
	}
	client.os = "windows"
	if _, err := client.Check(context.Background(), "0.17.0"); err == nil {
		t.Fatal("accepted unsupported platform")
	}
}

func TestInstallAtomicReplacementAndSymlink(t *testing.T) {
	client, _ := releaseServer(t, archiveBytes(t, "repocli", tar.TypeReg), "")
	release, err := client.Check(context.Background(), "0.17.0")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "actual")
	link := filepath.Join(dir, "repocli")
	if err := os.WriteFile(binary, []byte("old executable"), 0751); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(binary, link); err != nil {
		t.Fatal(err)
	}
	// An open descriptor observes the old inode even after the atomic replacement.
	old, err := os.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	if err := client.Install(context.Background(), release, link); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(link)
	if string(got) != "new executable" {
		t.Fatalf("installed %q", got)
	}
	var oldBytes [14]byte
	if _, err := old.Read(oldBytes[:]); err != nil || string(oldBytes[:]) != "old executable" {
		t.Fatalf("old inode changed: %q %v", oldBytes, err)
	}
	info, _ := os.Stat(binary)
	if info.Mode().Perm() != 0751 {
		t.Fatalf("mode %v", info.Mode())
	}
	if _, err := os.Readlink(link); err != nil {
		t.Fatal("replaced symlink instead of its target")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatalf("temporary files leaked: %v", entries)
	}
}

func TestFailedInstallPreservesBinary(t *testing.T) {
	valid := archiveBytes(t, "repocli", tar.TypeReg)
	for _, tc := range []struct {
		name     string
		archive  []byte
		checksum string
	}{
		{"checksum mismatch", valid, strings.Repeat("0", 64)},
		{"invalid checksum", valid, "bad"},
		{"invalid gzip", []byte("not gzip"), ""},
		{"truncated archive", valid[:len(valid)-5], ""},
		{"traversal", archiveBytes(t, "../repocli", tar.TypeReg), ""},
		{"symlink", archiveBytes(t, "repocli", tar.TypeSymlink), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := releaseServer(t, tc.archive, tc.checksum)
			release, err := client.Check(context.Background(), "0.17.0")
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			binary := filepath.Join(dir, "repocli")
			os.WriteFile(binary, []byte("old"), 0755)
			if err := client.Install(context.Background(), release, binary); err == nil {
				t.Fatal("expected failure")
			}
			got, _ := os.ReadFile(binary)
			if string(got) != "old" {
				t.Fatalf("old binary lost: %q", got)
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 1 {
				t.Fatal("temporary files leaked")
			}
		})
	}
}

func TestReleaseLookupFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"rate limit", "{}", 403}, {"malformed", "{", 200},
		{"prerelease", `{"tag_name":"v1.0.0-rc.1"}`, 200},
		{"draft", `{"tag_name":"v1.0.0","draft":true}`, 200},
		{"missing assets", `{"tag_name":"v1.0.0"}`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
			defer server.Close()
			client := &Client{http: server.Client(), url: server.URL, os: "linux", arch: "amd64"}
			if _, err := client.Check(context.Background(), "0.17.0"); err == nil {
				t.Fatal("expected lookup failure")
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	client := &Client{http: server.Client(), url: server.URL}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := client.Check(ctx, "0.17.0"); err == nil {
		t.Fatal("deadline ignored")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTokenOnlyAuthenticatesGitHubAPI(t *testing.T) {
	client := &Client{token: "test-token", http: &http.Client{}}
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		want := ""
		if r.URL.String() == "https://api.github.com/repos/compforge/repocli/releases/latest" {
			want = "Bearer test-token"
		}
		if got := r.Header.Get("Authorization"); got != want {
			t.Fatalf("%s authorization=%q", r.URL, got)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header)}, nil
	})
	for _, url := range []string{latestURL, "https://github.com/compforge/repocli/releases/download/v1/a", "http://api.github.com/test", "https://example.com/a"} {
		response, err := client.get(context.Background(), url)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
	}
}

func TestInstallRunningExecutable(t *testing.T) {
	payload := []byte("#!/bin/sh\nprintf 'repocli version 0.18.0\\n'\n")
	client, _ := releaseServer(t, archiveContent(t, "repocli", tar.TypeReg, payload), "")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(t.TempDir(), "repocli")
	if err := os.WriteFile(copyPath, data, 0755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(copyPath, "-test.run=^TestUpgradeHelperProcess$")
	command.Env = append(os.Environ(), "REPOCLI_UPGRADE_HELPER_URL="+client.url)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("self-replacement: %v: %s", err, out)
	}
	// Launch the same path again: it must now execute the verified release payload.
	out, err := exec.Command(copyPath, "version").CombinedOutput()
	if err != nil || string(out) != "repocli version 0.18.0\n" {
		t.Fatalf("new executable: %v %q", err, out)
	}
}

func TestUpgradeHelperProcess(t *testing.T) {
	url := os.Getenv("REPOCLI_UPGRADE_HELPER_URL")
	if url == "" {
		return
	}
	client := &Client{http: &http.Client{Timeout: 5 * time.Second}, url: url, os: "darwin", arch: "arm64"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release, err := client.Check(ctx, "0.17.0")
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Install(ctx, release, executable); err != nil {
		t.Fatal(err)
	}
}
