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
Python 的 `stage(..., validate=callback)` 先在隔离 index 中暂存，再把候选 `IndexView`
交给调用方验证。该视图同时提供相对 HEAD 的 changes、完整 entries，以及绑定这些条目 blob OID 的
配置查询；涉及多个文件的判断使用同一候选内容，不混用工作区文件。
验证抛错或 add 失败时，真实 index 保持原样，包括部分暂存；通过后才原子替换。
此模式允许空路径列表，用于验证已有暂存。整个过程持有 Git 原生 index lock，已有锁时拒绝操作。
回调只读检查，不执行 index 写操作；敏感文件、提交范围等策略由回调的调用方拥有。
gitlink 与其他路径一样作为 entry 返回，保留 mode 与 OID；是否要求注册由调用方决定。
保护范围仅限 index，不回滚 Git 对象写入或 clean filter 的外部副作用。
`commit` 提交当前 index。`push` 默认普通推送，可显式给出期望远端 SHA 的 lease。
`rebase` 解析明确的目标版本，冲突保留在工作区。Python 的 `continue_rebase` 继续已暂存的冲突解决，
可显式设置本次调用的 `core.editor`（Git 的 editor 环境变量仍优先）；`abort_rebase` 请求 Git 恢复
rebase 前的状态。两者保留 GitResult，包括失败与 uncertain，不自动重试或管理消费方事务。
是否继续、终止及何时推送，由调用方决定。
Worktree 的路径和分支由调用方给出，删除默认拒绝脏内容和锁定状态。

`git.create_branch` 从明确基线创建并切换分支，不隐式保存工作区。Python `stash.save` 返回本次保存的
对象 ID；`stash.restore` 按该 ID 恢复，并保留 index 与工作区的区别。恢复使用 apply，成功或冲突后
均保留 stash 备份，调用方可展示其 ID 供后续清理；库不按共享 stash 栈的位置删除条目。
是否携带改动、何时切分支及何时更新调用方状态，由工作流决定。

Forge 的合并状态表示平台观察；哪些状态需要提醒、等待或阻断开发流程，由消费方决定。

GitResult 保留退出码和输出；超时的 uncertain 表示结果可能已生效，不能盲目重试。
查询失败不能当作 clean：workspace status 包含 complete，不能观察完整时由调用方决定如何处理。
Git 传输默认有超时；Forge 客户端复用有超时的 HTTP 传输，不自动重试写操作。

Forge 构造参数显式指定 host、repository 与 token。创建/更新使用中立的标题、正文和分支参数；
合并要求 expected_sha，防止把检查之后新增的提交一并合入。客户端不替调用方决定是否允许合并。
GitHub/GitLab 的评论线程、引用与 resolution 由 adapter 解释，业务 finding 和 verdict 不进入库模型。

## 内容身份

Python `snapshot(repository)` 与 TypeScript `snapshot(repository, options)` 捕获工作区内容，返回
内容 digest、完整性与诊断。完整捕获采用 [snapshot](snapshot.md) 的 Go 摘要契约，覆盖中文路径、
普通文件、链接、gitlink 引用和大文件；捕获两次发现变化时返回 incomplete。
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

`git_path` / `gitPath` 由 Git 解析元数据的绝对路径，保留空格和换行，允许目标尚不存在；失败抛错。
Git 决定 `index`、`rebase-merge` 等 checkout-local 路径与 `info/exclude` 等共享路径的归属，
消费方无需猜测 `.git` 的布局。Python `rebase_in_progress` 观察当前 checkout 的两种 rebase backend；
状态目录不存在表示未进行，Git 查询或文件读取失败不能当成未进行。路径契约由 Python/TypeScript
共享语料验证。事务文件命名、内容、index 活跃度解释及 rebase 的 lease/验证/发布策略归消费方。

## 调用预算与不确定结果

Python `operation(timeout=..., cancel=...)` 为一组 Git/Forge 调用提供共享 deadline；嵌套 scope 不能延长
外层预算。Git 的单次默认上限仍生效；取消、超时或输出超限会终止并回收子进程。stdout 上限 16 MiB，
stderr 上限 64 KiB。启动前取消是明确未执行；启动后中断保留 GitResult.uncertain，不推断回滚。

```python
from repocli import git, operation

with operation(timeout=20):
    result = git.create_branch(repo, "feature", "origin/main")
```

Forge 写请求已发出后发生传输中断、5xx 或无法解码响应，抛出 ForgeOutcomeUnknown（ForgeError 子类），
调用方必须先核对远端状态。明确的 4xx 保留对应错误，库不自动重试。HTTP 单响应最多 16 MiB；
分页共用整个列表查询的预算，最多 10,000 项，失败不返回截断清单。调用方可用 operation 将多次
adapter 调用放在同一预算内。HTTP 取消在请求/读取边界检查，正在阻塞的 socket 受本次请求时限约束，
不是可立即打断的异步客户端。scope 不提供事务或补偿。

`find_checkout` / `findCheckout` / Go `FindCheckout` 查找物理路径祖先中的 `.git` 条目并由 Git 验证。
没有 checkout 返回 None/undefined/空字符串；路径无效、权限、损坏元数据或 Git 执行失败抛错。
路径保留尾部空格和换行。该查询不把 Git 元数据目录当源码 checkout，也不通过环境覆盖来选择其它仓库。

## Checkout 依赖准备（Python）

`inspect_dependencies(component_path)` 只读返回 `DependencyEnvironment`；`prepare_dependencies(environment)`
显式执行依赖安装。创建 worktree、inspect、snapshot 均不隐式准备环境；调用时机和验证门禁属于消费方。
本轮先提供 Python API，供 devloop 的原生 Python workflow 使用，不预建其它语言或 CLI 包装。

观察区分 `ready`（准备回执与当前输入一致）、`present`（本地环境存在但未经回执验证）、
`missing`、`stale` 和 `unsupported`。消费方可允许用户自管的 present 环境进入实际验证，但不能称其
锁文件一致性已经验证。显式 prepare 会使用支持的锁定安装方式，并在成功后写环境目录内的回执。
无对应依赖声明的 Component 返回空列表；不会尝试安装语言运行时。

安装支持 npm ci、pnpm/Bun frozen install、Yarn classic frozen / modern immutable 和 uv sync --locked。
不生成或更新锁文件。没有支持的锁定安装入口时仍能观察已存在的本地环境，缺失时明确 unsupported。
Yarn PnP 暂不支持。Node package.json workspaces、pnpm-workspace.yaml 与 uv workspace 成员解析到本 checkout
内的安装根；嵌套 Component 不重复安装整套 workspace。成员排除生效，暂不支持 brace/extglob 成员表达式。

回执绑定绝对安装路径、锁文件、workspace 成员 manifest 与安装配置；成员新增或修改会使回执失效。
整个 node_modules/.venv 目录的外部链接不会被当成本地就绪环境，uv 不继承其它 checkout 的环境路径覆盖。
多个线程按安装根串行、锁内复查；默认安装预算 600 秒，受调用方 operation scope 的更短预算与取消控制。
输出有容量限制，中断会终止安装进程组，返回 uncertain，不宣称副作用已回滚。跨进程锁不在本 API 保证范围内。
安装非零退出、超时、未生成本地依赖或安装期间输入变化均不写 ready 回执，返回原因与可用的进程证据。
