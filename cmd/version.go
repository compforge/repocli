package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func newVersionCommand(opts *options, version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show the installed binary version",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			var err error
			if opts.json {
				err = json.NewEncoder(command.OutOrStdout()).Encode(map[string]string{"version": version})
			} else {
				_, err = fmt.Fprintf(command.OutOrStdout(), "repocli version %s\n", version)
			}
			if err != nil {
				return executionError{err}
			}
			return nil
		},
	}
}
