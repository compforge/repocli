# 仓库操作工具包

调用方需要把仓库动作组合成工作流，repocli 提供其中可复用的观察和操作。任务名、工作区保留策略、
验证结果和 review 进度属于调用方；一次 Git 操作的参数、实际结果和平台协议由工具包负责。

## 使用方式

Python 提供 `repocli.git`、`repocli.git_state` 和 `repocli.forge`；TypeScript 提供 Git 查询与中立的
Forge 类型。两种语言都直接提供工作区 `snapshot`；不启动 repocli CLI，也不要求分析器参与写操作。

```python
from repocli import git, snapshot
from repocli.forge import GitHubForge

observed = snapshot("/path/to/checkout")
if not observed.complete:
    raise RuntimeError(observed.diagnostics)

result = git.stage("/path/to/checkout", ["src/example.py"])
if not result.ok:
    raise RuntimeError(result.err)
# 调用方核对 index 范围与验证结果后，再决定是否 commit / push。
```

`stage` 使用 literal pathspec，要求非空的显式路径；它不清空调用方已有的 index。
`commit` 提交当前 index。`push` 默认普通推送，可显式给出期望远端 SHA 的 lease。
`rebase` 解析明确的目标版本，冲突保留在工作区，由调用方决定解决、继续或终止。
Worktree 的路径和分支由调用方给出，删除默认拒绝脏内容和锁定状态。

GitResult 保留退出码和输出；超时的 uncertain 表示结果可能已生效，不能盲目重试。
查询失败不能当作 clean：workspace status 包含 complete，不能观察完整时由调用方决定如何处理。
Git 传输默认有超时；Forge 客户端复用有超时的 HTTP 传输，不自动重试写操作。

Forge 构造参数显式指定 host、repository 与 token。创建/更新使用中立的标题、正文和分支参数；
合并要求 expected_sha，防止把检查之后新增的提交一并合入。客户端不替调用方决定是否允许合并。
GitHub/GitLab 的评论线程、引用与 resolution 由 adapter 解释，业务 finding 和 verdict 不进入库模型。

## 内容身份

Python `snapshot(repository)` 与 TypeScript `snapshot(repository, options)` 捕获工作区内容，返回
内容 digest、完整性与诊断。完整捕获采用 [snapshot](snapshot.md) 的 Go 摘要契约，覆盖中文路径、
普通文件、链接、已初始化 submodule 和大文件；捕获两次发现变化时返回 incomplete。
当前 TS/Python 接口只接受工作区输入；index/commit 输入由 Go API 提供。

stamp、缓存复用与检查覆盖范围属于消费者。完整仓库摘要是保守绑定：任一被捕获输入变化均使旧结果
需要重新判断，不能把“相对 HEAD 没有改动”误认为“和上次验证内容相同”。

## Git 事实与 checkout 拓扑

Python 的 `committed_paths` / `range_paths` 与 TypeScript 的 `committedPaths` / `rangePaths`
返回原始仓相对路径，保留 Unicode、空格、换行及 rename 两端。单 commit 按第一父提交比较，
根 commit 与空树比较；分支范围按 merge base 比较。查询失败抛错，不能解释成没有改动。
`parse_remote_url` / `parseRemoteUrl` 统一解析 HTTPS、SSH URL 和 scp 格式；结果只有 host/path，
不保留凭据，也不选择平台、API 地址或认证方式。

`checkout_info` / `checkoutInfo` 返回当前 checkout、独立 Git 元数据、共享元数据和可确认的主 checkout。
`list_worktrees` / `listWorktrees` 保留 Git 注册记录，使用 NUL 分隔以完整保留路径；失败抛错。
注册记录的第一项可能是 bare 或独立 Git 目录，不能直接当作主 checkout。
独立 Git 目录没有反向记录原 checkout 时，`main_root` / `mainRoot` 为未知；调用方决定共享状态
放在可确认的主 checkout 还是 common directory。库不创建 `.devloop`、Session 或清理计划。

Go 消费方直接使用 `ListWorktrees`、`Checkout`、`AddWorktree`、`RemoveWorktree`。
查询接受 context，Git 读取每次最多 5 秒，写操作最多 30 秒；调用方更短的时限优先。
Go `GitResult.Err` 表示操作错误，`Uncertain` 表示启动后取消导致结果不确定；
Python 使用 `ok` / `uncertain`。新分支与 detached 模式互斥，remove 默认保护脏内容与锁定 checkout。
CCR 可以据此组织评审 checkout，devloop 可以组织开发 checkout；命名、占用、保留期限和清理授权
均属于消费方。TypeScript 当前提供查询，写操作按实际消费需求扩展。
