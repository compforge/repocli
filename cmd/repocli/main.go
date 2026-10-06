// Command repocli exposes the repository toolkit as a CLI.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/compforge/repocli"
	"github.com/compforge/repocli/internal/cli"
)

func main() {
	cli.Version = repocli.Version()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	os.Exit(cli.Execute(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
