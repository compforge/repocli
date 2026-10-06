# Repository toolkit for Go

Import `github.com/compforge/repocli/toolkit/go` (package `repocli`) to inspect organization,
capture content identity, analyze changes and build code graphs in your process.

```go
import repocli "github.com/compforge/repocli/toolkit/go"

report, err := repocli.Inspect(ctx, repocli.InputRequest{Repository: repoPath})
```

Check the error, then `report.Complete` and `report.Diagnostics`. `report.Owner(path)` returns
the deepest owning component for a repository-relative path, including a deleted file.
Repository, Component and Product identities come from quality-harness common.

Calls may invoke Git. They do not launch the CLI, execute project commands or persist CLI history.
Callers own context deadlines, output and policy. See [repository inspection](../../docs/repository.md)
and the [project overview](../../README.md) for the other APIs and their limits.

Within this repository, `go.work` selects the local toolkit and CLI modules. Run `make lint`,
`make test` and `make build` here for this component. Contract tests use the shared repository
fixtures; the library itself can be built independently with `GOWORK=off go build ./...`.
