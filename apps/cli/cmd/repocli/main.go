// Command repocli exposes the repository toolkit as a CLI.
package main

import (
	"context"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"

	"github.com/compforge/repocli/apps/cli/internal/cli"
)

func main() {
	// Source/release builds inject VERSION; go install @version supplies module metadata.
	if cli.Version == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "(devel)" && info.Main.Version != "" {
			cli.Version = strings.TrimPrefix(info.Main.Version, "v")
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	os.Exit(cli.Execute(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
