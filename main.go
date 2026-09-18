package main

import (
	"context"
	_ "embed"
	"os"
	"os/signal"
	"strings"

	"github.com/compforge/repocli/cmd"
)

//go:embed VERSION
var version string

func main() {
	// Embed the repository version so go build and go install share one source.
	cmd.Version = strings.TrimSpace(version)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	os.Exit(cmd.Execute(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
