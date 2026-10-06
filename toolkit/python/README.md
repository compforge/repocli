# repocli for Python

Native organization inspection for Git repositories: discover Components, language and
package-tool evidence, then locate the owner of a repository-relative path.
Python 3.11+ on macOS/Linux and Git on PATH are required. Organization analysis runs
in Python; Git provides version-control facts through batched commands.

Install from the repository checkout:

```sh
python -m pip install ./toolkit/python
```

```python
from repocli import inspect, owner

report = inspect("/path/to/repo", timeout=5)
if not report.complete:
    raise RuntimeError(report.diagnostics)
component = owner(report, "server/main.py")
if component is not None:
    print(component.name, component.root, component.language)
```

`inspect(..., staged=True)` selects the index; `inspect(..., head="HEAD")` selects a
resolved commit. The default observes the working tree, including non-ignored untracked
paths. `timeout` is a whole-call budget in seconds; an optional `threading.Event` passed
as `cancel` requests cancellation. Git processes are terminated and reaped on timeout,
cancellation or output overflow. Filesystem reads and Python analysis check the budget
between operations. Timeout raises `TimeoutError`, cancellation raises `InterruptedError`,
invalid selections/configuration or limits raise `ValueError`, and filesystem/Git failures
propagate `OSError` / `subprocess.CalledProcessError`.

Reports and their nested collections are immutable dataclasses and tuples. Shared
Repository, Component, Product and Forge identities come from `harness-common`;
ComponentBinding adds checkout layout and tool evidence. `owner` accepts a repository-relative
path and returns the deepest binding or None, including for deleted paths.
Python properties use snake_case; these objects are not a CLI JSON protocol adapter.

Inspection reads filenames and necessary metadata, not source contents or ASTs. It never
runs project code or installs dependencies. `complete` concerns observed organization;
concurrent metadata changes return an incomplete report with diagnostics. Consumers own
acceptance, command selection and validation policy. Snapshot, diff and graph analysis
remain available through the Go toolkit / CLI.

Development: `uv sync --locked`, then `make fix`, `make lint`, `make test`, `make build`.
Tests consume the shared `conformance/inspect` cases and generated filename probes.
See [the inspection contract](../../docs/repository.md).

## Repository toolkit

工具包提供仓库组织、内容身份与 Git 查询。Python 还提供 worktree、提交、推送、rebase 和
GitHub/GitLab 的 PR/MR 操作；调用方直接导入库，组织自己的开发流程。
分析接口保持只读，操作接口接收明确参数，不隐式执行验证或管理任务状态。

详见 [操作契约](../../docs/operations.md)。
