# repocli for TypeScript

Inspect a Git repository directly in a Node.js process. Discover component boundaries,
languages and package-tool evidence without loading source contents or running project code.
Node.js 22+ and Git are required; the repocli executable and Go toolchain are not needed at runtime.

```typescript
import { inspect, owner } from "@compforge/repocli";

const report = await inspect({ repository: repoPath, signal, timeoutMs: 5_000 });
if (!report.complete) throw new Error(JSON.stringify(report.diagnostics));
const component = owner(report, "server/main.go"); // undefined when unowned
```

Use `staged: true` for the index or `head: "HEAD"` for a resolved commit. They are mutually
exclusive; the default is the working tree. One timeout (default five seconds) covers the
whole call. Invalid configuration, read failures, limits and cancellation reject the promise;
observed concurrent changes return an incomplete report with diagnostics.

Repository, Component and Product types come from `@compforge/harness-common`.
`owner` accepts repository-relative paths, including deleted files. The result serializes to
Go inspect's schema 1. Its completeness describes organization metadata, not buildability or
content identity. See [the shared contract](../../docs/repository.md).

This package provides inspection and ownership only. The Go API separately provides snapshot,
diff and graph analysis. Logging, persistence, validation policy and handling partial results
belong to callers.

## Development

Run `npm install`, `make lint` and `make test` in this directory. `make build` produces ESM
and type declarations in `dist/`. Runtime Git calls and metadata reads are bounded; tests use
isolated local repositories and need no remote service. `../../conformance/inspect` supplies the
same organization and version-selection cases to Go and TypeScript.

The dependency `@compforge/harness-common@^0.3.5` must be published before a registry install
or release. While reviewing both repositories together, build and `npm pack` the common package,
then install that tarball with `npm install --no-save --package-lock=false <tarball>`. Do not
commit local paths as dependencies.

The filename catalog is generated from the Go module's pinned CodeGraph/gotreesitter metadata.
After upgrading those dependencies, run `make generate-languages` at the repository root;
Go tests detect stale metadata and TS tests check the upstream filename probes.

## Repository toolkit

工具包提供仓库组织、内容身份与 Git 查询。Python 还提供 worktree、提交、推送、rebase 和
GitHub/GitLab 的 PR/MR 操作；调用方直接导入库，组织自己的开发流程。
分析接口保持只读，操作接口接收明确参数，不隐式执行验证或管理任务状态。

详见 [操作契约](../../docs/operations.md)。

## Repository entries

`tree` lists directories, files, Git entry kinds and known roles independently of
Component discovery, for working-tree, index and commit inputs. Project Manifest
files retain their ecosystem; Component bindings retain `manifests` as path evidence.
Components provide the granularity for lint, test and packaging operations. Discovery prefers
manifests, then Makefiles with unknown language. Without either, the repository has no
recognized Component; no root Component or tool-specific configuration file is created.
See [repository contract](../../docs/repository.md) and [snapshot v2](../../docs/snapshot.md).
