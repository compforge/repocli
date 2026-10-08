package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/compforge/repocli/apps/cli/internal/upgrade"
)

type fakeUpgrade struct {
	installCalls           int
	failCheck, failInstall bool
	available              bool
}

func (f *fakeUpgrade) Check(ctx context.Context, version string) (upgrade.Release, error) {
	if _, ok := ctx.Deadline(); !ok {
		return upgrade.Release{}, errors.New("missing deadline")
	}
	if f.failCheck {
		return upgrade.Release{}, errors.New("lookup failed")
	}
	return upgrade.Release{CurrentVersion: version, LatestVersion: "0.18.0", UpdateAvailable: f.available}, nil
}
func (f *fakeUpgrade) Install(ctx context.Context, release upgrade.Release, path string) error {
	f.installCalls++
	if f.failInstall {
		return errors.New("installation failed")
	}
	return nil
}

func TestUpgradeCommand(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		args                              []string
		available, failCheck, failInstall bool
		code, installs                    int
	}{
		{"check JSON", []string{"upgrade", "--check", "--json"}, true, false, false, 0, 0},
		{"upgrade JSON", []string{"upgrade", "--json"}, true, false, false, 0, 1},
		{"no downgrade", []string{"upgrade", "--json"}, false, false, false, 0, 0},
		{"lookup failure", []string{"upgrade", "--check", "--json"}, true, true, false, 1, 0},
		{"install failure", []string{"upgrade", "--json"}, true, false, true, 1, 1},
		{"bad argument", []string{"upgrade", "extra"}, true, false, false, 2, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			opts := &options{}
			root := newRootCommand(opts)
			for _, cmd := range root.Commands() {
				if cmd.Name() == "upgrade" {
					root.RemoveCommand(cmd)
				}
			}
			client := &fakeUpgrade{available: tc.available, failCheck: tc.failCheck, failInstall: tc.failInstall}
			root.AddCommand(newUpgradeCommand(opts, "0.17.0", client))
			var out, stderr bytes.Buffer
			code := execute(context.Background(), root, tc.args, nil, &out, &stderr)
			if code != tc.code || client.installCalls != tc.installs {
				t.Fatalf("exit=%d installs=%d stderr=%s", code, client.installCalls, stderr.String())
			}
			if code == 0 {
				var result struct {
					CurrentVersion  string `json:"currentVersion"`
					UpdateAvailable bool   `json:"updateAvailable"`
					Updated         bool   `json:"updated"`
				}
				if err := json.Unmarshal(out.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.CurrentVersion != "0.17.0" || result.UpdateAvailable != tc.available || result.Updated != (tc.installs == 1) {
					t.Fatalf("bad report %+v", result)
				}
			} else if out.Len() != 0 {
				t.Fatalf("failure wrote success report: %s", out.String())
			}
		})
	}
}
