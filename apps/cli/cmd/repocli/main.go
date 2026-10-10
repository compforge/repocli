// Command repocli exposes the repository toolkit as a CLI.
package main

import (
	"context"
	"fmt"
	"github.com/compforge/go-stdx/timeline"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"time"

	"github.com/compforge/repocli/apps/cli/internal/cli"
)

func main() { os.Exit(run()) }

func run() int {
	// Source/release builds inject VERSION; go install @version supplies module metadata.
	if cli.Version == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "(devel)" && info.Main.Version != "" {
			cli.Version = strings.TrimPrefix(info.Main.Version, "v")
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	manager, err := timeline.NewManager(nil, timeline.Config{Actor: timeline.Actor{ID: "repocli"}})
	if err != nil {
		fmt.Fprintln(os.Stderr, "repocli: timeline:", err)
		return 1
	}
	timeline.SetDefault(manager)
	defer func() {
		closeCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := manager.Shutdown(closeCtx); err != nil {
			fmt.Fprintln(os.Stderr, "repocli: timeline shutdown:", err)
		}
	}()
	return cli.Execute(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
}
