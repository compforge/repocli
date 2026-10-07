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
不保留凭据，也不选择平台、API 地址或认证方式。身份解析接受带 host/path 的非 file URL，
包括 `git+ssh`、`git+https`、`ssh+git` 等 scheme；是否允许或能够执行该传输由 Git 操作决定。

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

Python `git_index` 提供逐文件 status、工作区删除路径、带前后 mode 的 index changes，以及工作区
`.gitmodules` 中的注册路径。路径按 NUL 分隔读取，保留空白、换行和 rename 两端；查询失败抛错，
不能当作没有改动。是否暂存、是否允许 gitlink 及如何处理敏感文件由消费方决定。

`list_checkouts` / `listCheckouts` / Go `ListCheckouts` 将 Git 注册记录解释为 checkout 清单。独立元数据目录无法反查主工作区时，
对应项保留未知 path；从主工作区自身调用时可使用当前 checkout 定位。消费方不能把未知位置当成
checkout 已回收，也不能把元数据目录作为工作区操作。

本地分支与远端分支 tip 查询的空集合表示成功读取且无匹配项；Git 执行失败抛错。Checkout 查询也
区分未知主工作区位置与查询失败，兼容的 metadata/linked 查询复用同一拓扑模型。bare 注册项不构成
checkout。Forge 多页查询只在所有页面均符合列表协议时返回，格式错误或读取失败不返回部分清单。
消费方决定是否保留旧观察、展示未知或停止操作。三种语言共用 `conformance/git/checkouts.json` 验证
checkout 语义；原始 worktree 注册记录用于底层诊断，工作区消费者使用 checkout 清单。

单值查询也保留缺失与失败的区别：current branch 的空值表示 detached HEAD；HEAD 的空值表示 unborn；
revision 的空字符串表示 quiet verification 确认无法解析该 revision。ahead/behind 的空值表示缺少
比较端点或 upstream，不表示 0/0。执行失败与格式错误抛错。祖先判断仅在 Git 返回 0/1 时返回
true/false，缺失对象和执行失败抛错；即使两个输入字符串相等也必须验证。PR 选择、监控缓存及
写入前如何处理缺口属于消费方。Python/TypeScript 共用 scalar query 契约语料，不为语言对齐扩展无消费者的 API。
