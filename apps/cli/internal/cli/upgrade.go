package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/compforge/repocli/apps/cli/internal/upgrade"
	"github.com/spf13/cobra"
)

type upgradeClient interface {
	Check(context.Context, string) (upgrade.Release, error)
	Install(context.Context, upgrade.Release, string) error
}

func newUpgradeCommand(opts *options, version string, client upgradeClient) *cobra.Command {
	var check bool
	command := &cobra.Command{
		Use:   "upgrade",
		Short: "Check for or install the latest stable CLI release",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(command.Context(), opts.timeout)
			defer cancel()
			release, err := client.Check(ctx, version)
			if err != nil {
				return executionError{err}
			}
			updated := false
			if !check && release.UpdateAvailable {
				executable, err := os.Executable()
				if err != nil {
					return executionError{err}
				}
				if err := client.Install(ctx, release, executable); err != nil {
					return executionError{err}
				}
				updated = true
			}
			if opts.json {
				err = json.NewEncoder(command.OutOrStdout()).Encode(struct {
					upgrade.Release
					Updated bool `json:"updated"`
				}{release, updated})
			} else if updated {
				_, err = fmt.Fprintf(command.OutOrStdout(), "Upgraded repocli %s -> %s\n", release.CurrentVersion, release.LatestVersion)
			} else if release.UpdateAvailable {
				_, err = fmt.Fprintf(command.OutOrStdout(), "repocli %s is available (installed %s); run repocli upgrade\n", release.LatestVersion, release.CurrentVersion)
			} else {
				_, err = fmt.Fprintf(command.OutOrStdout(), "repocli %s is up to date (latest stable %s)\n", release.CurrentVersion, release.LatestVersion)
			}
			if err != nil {
				return executionError{err}
			}
			return nil
		},
	}
	command.Flags().BoolVar(&check, "check", false, "only check for updates; do not change the installed binary")
	return command
}
