// Package upgrade owns explicit CLI updates; analysis never checks for updates.
package upgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const latestURL = "https://api.github.com/repos/compforge/repocli/releases/latest"

// Client shares a bounded HTTP client across release lookup and asset downloads.
type Client struct {
	token    string
	http     *http.Client
	url      string
	os, arch string
}

func NewClient() *Client {
	token := os.Getenv("GH_TOKEN")
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	return &Client{http: &http.Client{Timeout: 30 * time.Second}, url: latestURL, os: runtime.GOOS, arch: runtime.GOARCH, token: token}
}

// Release is a checked stable release and its platform-specific installation assets.
type Release struct {
	CurrentVersion                   string `json:"currentVersion"`
	LatestVersion                    string `json:"latestVersion"`
	UpdateAvailable                  bool   `json:"updateAvailable"`
	assetName, assetURL, checksumURL string
}

// Check discovers a stable release without inspecting or modifying the installed binary.
func (c *Client) Check(ctx context.Context, current string) (Release, error) {
	result := Release{CurrentVersion: current}
	current = "v" + strings.TrimPrefix(current, "v")
	if !semver.IsValid(current) {
		return result, fmt.Errorf("cannot compare development version %q; install a versioned repocli release first", result.CurrentVersion)
	}
	var manifest struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	response, err := c.get(ctx, c.url)
	if err != nil {
		return result, fmt.Errorf("check latest release: %w", err)
	}
	defer response.Body.Close()
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&manifest); err != nil {
		return result, fmt.Errorf("decode latest release: %w", err)
	}
	if !semver.IsValid(manifest.Tag) || semver.Prerelease(manifest.Tag) != "" || manifest.Draft || manifest.Prerelease {
		return result, fmt.Errorf("latest release has no stable semantic version: %q", manifest.Tag)
	}
	result.LatestVersion = strings.TrimPrefix(manifest.Tag, "v")
	result.UpdateAvailable = semver.Compare(manifest.Tag, current) > 0
	if !result.UpdateAvailable {
		return result, nil
	}
	if c.os != "darwin" && c.os != "linux" {
		return result, fmt.Errorf("self-upgrade is unsupported on %s/%s", c.os, c.arch)
	}
	result.assetName = fmt.Sprintf("repocli_%s_%s_%s.tar.gz", manifest.Tag, c.os, c.arch)
	for _, asset := range manifest.Assets {
		switch asset.Name {
		case result.assetName:
			result.assetURL = asset.URL
		case "checksums.txt":
			result.checksumURL = asset.URL
		}
	}
	if result.assetURL == "" || result.checksumURL == "" {
		return result, fmt.Errorf("release %s is missing %s or checksums.txt", manifest.Tag, result.assetName)
	}
	return result, nil
}

func (c *Client) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "repocli-upgrade")
	// Tokens authenticate only the official API, never release download hosts.
	if c.token != "" && req.URL.Scheme == "https" && req.URL.Host == "api.github.com" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		if response.StatusCode == http.StatusForbidden && response.Header.Get("X-RateLimit-Remaining") == "0" {
			return nil, fmt.Errorf("GitHub API rate limit exceeded; retry later or set GH_TOKEN/GITHUB_TOKEN")
		}
		return nil, fmt.Errorf("GET %s: HTTP %d", url, response.StatusCode)
	}
	return response, nil
}
