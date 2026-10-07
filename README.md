# repocli

[中文](README.zh-CN.md)

A toolkit for querying, analyzing and operating on Git repositories, with Go, TypeScript and Python APIs and a CLI for
developers, scripts, and coding agents. It discovers repository organization, identifies
contents, analyzes changes, and builds code graphs. Libraries provide composable Git/Forge operations; the CLI exposes selected queries
and adds a local graph browser.

| Command | Use it to |
|---|---|
| `tree` | List directories, files and known roles without requiring components |
| `inspect` | Discover components, languages and package-tool evidence |
| `snapshot` | Identify repository contents and capture completeness |
| `diff` | Explain changes and possible file/test impact |
| `view` | Browse the code graph and its captured source |

## Installation

Requires Go 1.26+ and Git. Install from source:

```sh
git clone https://github.com/compforge/repocli.git
cd repocli
make install
```

Tagged [releases](https://github.com/compforge/repocli/releases) provide macOS/Linux archives
and checksums. Use `repocli version` or `repocli --version` to identify the installed
binary; `repocli version --json` produces structured output. Both source and release
builds inject the version from the repository’s `VERSION` file.

`make install` uses `go install`: the binary goes to `GOBIN`, or `$(go env GOPATH)/bin`
when `GOBIN` is unset. Ensure that directory is on `PATH`. Use `make -C apps/cli build` to produce `bin/repocli`. Root `make build` builds all
components and requires Node.js, Python/uv and installed toolkit dependencies.

## Go API

Import `github.com/compforge/repocli/toolkit/go` to run repository analysis in your process:

```go
report, err := repocli.Inspect(ctx, repocli.InputRequest{Repository: repoPath})
if err != nil {
    return err
}
if !report.Complete {
    // Decide how to handle report.Diagnostics before using partial organization.
    return fmt.Errorf("repository inspection incomplete: %v", report.Diagnostics)
}
component := report.Owner("server/main.go") // nil when no component owns the path
```

`Tree` returns repository entries; `Inspect` returns organization; `Snapshot` returns content identity; `Diff` reports
changes and possible impact; `Graph` returns nodes, relations and their captured source.
Pass a context with the required deadline. Calls may invoke Git, but do not launch the
repocli binary, run project commands, or write CLI logs/history. Results retain completeness
and diagnostics so the caller can choose its policy. See [toolkit boundaries](docs/kernel.md).

The Go toolkit lives in `toolkit/go`; the CLI is a separate module in `apps/cli`.
Use the repository workspace for joint development. See [build and release details](docs/release.md).

## TypeScript API

`@compforge/repocli` provides native repository inspection for Node.js 22+:

```typescript
import { inspect, owner } from "@compforge/repocli";

const report = await inspect({ repository: repoPath, timeoutMs: 5_000 });
if (!report.complete) throw new Error(JSON.stringify(report.diagnostics));
const component = owner(report, "server/main.go");
```

It calls Git directly and reuses quality-harness common identities. It requires no repocli
binary or Go runtime. See [TypeScript setup and scope](toolkit/typescript/README.md); native working-tree snapshots and Git queries are also available. Diff and graph
analysis are provided by the Go API.

## Python API

The native Python toolkit provides tree/organization queries, working-tree snapshots and Git/Forge operations for Python 3.11+ on macOS/Linux:

```python
from repocli import inspect, owner

report = inspect("/path/to/repo", timeout=5)
if not report.complete:
    raise RuntimeError(report.diagnostics)
component = owner(report, "server/main.py")
```

Install from `toolkit/python`; see [Python setup and scope](toolkit/python/README.md).
It requires Git on PATH, shares common identities and uses the same inspection contract.

## Usage

`diff` reports changed source files and symbols, potentially affected files,
and component context. Declaration discovery follows CodeGraph language capabilities;
dependency-aware test selection supports Go, Python, JavaScript, and TypeScript.

```sh
repocli diff --repo /path/to/repo --base main --test-dir tests --json
```

Omit `--json` for readable text. Repeat `--test-dir` for multiple directories;
use `--test-dir .` for tests alongside source files. Without it, all supported source files become candidate roots, within the workset budget.
`--base` defaults to `HEAD` and compares that commit with the working tree.

Test impact is best effort: known-target inferred edges participate in recommendations,
with confidence and basis retained on explanation edges. Granularity is automatic; each
affected file reports its seed, path confidence and dependency distance. Local extraction
gaps remain observations; unknown targets do not create edges. An empty
test list does not prove that no tests are affected. Callers decide how to use the result. `diff`
does not run project commands. See [diff usage](docs/diff-usage.md) for patch input,
output fields, and analysis limits.

`view` opens a local web server for the working tree's code graph:

```sh
repocli view
repocli view --repo /path/to/repo --addr 127.0.0.1:5484
```

Open the printed URL to explore documents, search symbols, filter relationships by
kind and confidence, and inspect captured source and diagnostics. Use **Refresh snapshot**
after edits. Cytoscape.js and all page assets are embedded; no Node.js, CDN, or database
is needed at runtime. See [graph viewer](docs/view.md) for scope and limits.

`inspect` describes repository organization without reading source contents or hashing the repository:

```sh
repocli inspect --json
repocli inspect --staged --json
repocli inspect --head HEAD --json
```

It reports repository identity, component roots, languages and package-tool evidence from the selected
input. See [repository inspection](docs/repository.md) for discovery rules and limits.

`snapshot` identifies repository contents without running change or impact analysis:

```sh
repocli snapshot --json
repocli snapshot --staged --json
repocli snapshot --head HEAD --json
```

Its digest uses the same rules as `diff`; check `complete` before comparing it.
Snapshot schema 2 reports content identity only. Consumers of its former component fields should use `inspect`.
See [snapshot usage](docs/snapshot.md) for scope and limitations.

Analysis commands and invalid invocations automatically log replay arguments, version, elapsed time and exit status
to `~/.repocli/logs/YYYY-MM-DD.log`, keeping today and the previous 29 days.
Each recorded run has a `run_id`; output destinations stay unchanged.
Successful help/version commands do not initialize or write log files.
Diff analyses also append comparison inputs and stage timings to `diff-YYYY-MM-DD.jsonl`
in the same directory. See [execution logs](docs/logging.md).

## Repository toolkit

工具包提供仓库组织、内容身份与 Git 查询。Python 还提供 worktree、提交、推送、rebase 和
GitHub/GitLab 的 PR/MR 操作；调用方直接导入库，组织自己的开发流程。
分析接口保持只读，操作接口接收明确参数，不隐式执行验证或管理任务状态。

详见 [操作契约](docs/operations.md)。
