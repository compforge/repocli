package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/compforge/repocli/cmd"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	os.Exit(cmd.Execute(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
