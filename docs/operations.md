# 仓库操作工具包

调用方需要把仓库动作组合成工作流，repocli 提供其中可复用的观察和操作。任务名、工作区保留策略、
验证结果和 review 进度属于调用方；一次 Git 操作的参数、实际结果和平台协议由工具包负责。

## 使用方式

Python 提供 `repocli.git`、`repocli.git_state` 和 `repocli.forge`；TypeScript 提供 Git 查询与原生只读 Forge 客户端。两种语言都直接提供工作区 `snapshot`；不启动 repocli CLI，也不要求分析器参与写操作。

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
Python `git.prune_worktrees` 显式请求 Git 清理失效注册，沿用 Git 的过期及锁定保护规则；
返回 GitResult，不删除 checkout 目录或分支，清理时机与失败处理由调用方决定。

Python `git.add_exclude(repo, pattern)` 向 Git `info/exclude` 幂等追加一条精确规则，返回是否新增。
元数据路径由 Git 解析，适用于 linked worktree 和独立 Git 目录；保留原文件字节及规则空白，
读写失败向上传递。选择规则与 best-effort 策略属于调用方，库不内置任何应用目录。

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

### Forge 观察与请求协调

`open_pull_requests()` 完整分页读取当前仓库发起、且目标也在当前仓库的开放 PR/MR，按创建时间倒序。
与默认 `prs_for_branch()` 一样排除其它源仓的同名分支。分页失败抛错，不返回部分清单；一个 PR
不再出现在开放列表中，并不证明它已经合并。调用方通过 `get()` 确认变化，并决定历史补查频率。

GitHubForge / GitLabForge 可显式接收 `state_dir`。同一目录、host 和凭据的请求跨线程/进程串行，
锁等待受 operation deadline 约束。状态只保存凭据摘要；目录和缓存文件限制为当前用户访问。
不同凭据分别隔离；同一账号的不同 token 可能共享平台额度，当前不会推断它们属于同一主体。
目录由调用方选择，库不依赖应用配置或应用状态目录。未提供目录时不持久化缓存或共享冷却。

GET 条件缓存按完整 URL、参数及请求头区分，每次读取仍向服务端验证 ETag。304 复用对应响应体，
所有分页仍逐页验证，因此旧页评论编辑可见。POST/PUT/PATCH 不使用响应缓存；GraphQL 查询保留
既有协议。`request_counts()` 返回本客户端实际请求数和 304 数，日志记录资源和冷却时间。

`ForgeRateLimited` 区别于鉴权失败，携带 resource、retry_at 和 secondary。HTTP 403/429 及
GraphQL 200 的 RATE_LIMITED 错误均可分类；主配额按 resource 分开冷却，二级限制阻止同一凭据的
全部请求。Retry-After / reset 用于决定恢复时间，连续二级限制指数退避；库不会睡到恢复时间或自动
重试写操作。调用方保留上次成功观察和它的时间，另行报告刷新失败，不把缓存变成新的权威证据。

## 内容身份

Python `snapshot(repository)` 与 TypeScript `snapshot(repository, options)` 捕获工作区内容，返回
内容 digest、完整性与诊断。完整捕获采用 [snapshot](snapshot.md) 的 Go 摘要契约，覆盖中文路径、
普通文件、链接、gitlink 引用和大文件；捕获两次发现变化时返回 incomplete。
当前 TS/Python 接口只接受工作区输入；index/commit 输入由 Go API 提供。

stamp、缓存复用与检查覆盖范围属于消费者。完整仓库摘要是保守绑定：任一被捕获输入变化均使旧结果
需要重新判断，不能把“相对 HEAD 没有改动”误认为“和上次验证内容相同”。

## Git 事实与 checkout 拓扑

Python `git_state.local_default_branch(repo, remote="origin")` 只读取本地缓存的远端 HEAD，
保留含斜线的分支名；缺失返回 None，Git 失败抛错。该查询不访问网络、不猜默认分支，
刷新缓存和缺失时的 fallback 由调用方决定。

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
不生成或更新锁文件。环境状态与安装能力分开表达：目录不存在时为 `missing`；没有支持的锁定安装入口时
`command` 为空，显式 prepare 返回 `unsupported`。目录软链接或非目录占位为环境 `unsupported`；
观察错误直接报告，不能当作目录缺失。消费方依据这些事实决定是否直接运行项目检查。
Yarn PnP 暂不支持。Node package.json workspaces、pnpm-workspace.yaml 与 uv workspace 成员解析到本 checkout
内的安装根；嵌套 Component 不重复安装整套 workspace。成员排除生效，暂不支持 brace/extglob 成员表达式。

回执绑定绝对安装路径、锁文件、workspace 成员 manifest 与安装配置；成员新增或修改会使回执失效。
整个 node_modules/.venv 目录的外部链接不会被当成本地就绪环境，uv 不继承其它 checkout 的环境路径覆盖。
多个线程按安装根串行、锁内复查；默认安装预算 600 秒，受调用方 operation scope 的更短预算与取消控制。
输出有容量限制，中断会终止安装进程组，返回 uncertain，不宣称副作用已回滚。跨进程锁不在本 API 保证范围内。
安装非零退出、超时、未生成本地依赖或安装期间输入变化均不写 ready 回执，返回原因与可用的进程证据。

## Fork 与跨仓 PR

Python `Forge.repository()` 返回 common Repository，包括可空的 `forked_from` 直接父仓路径；
它限定在同一个 Forge 实例，查询错误直接传播。GitHub 使用 parent，GitLab 使用
forked_from_project；父仓与 fork 网络根仓不是同一概念。

`for_repository(path)` 选择 PR 编号、读取和更新所属的仓库。
`create(source_repository, source_branch, target_repository, target_branch, ...)` 显式接收四元组；
两个 repository 均为 common Repository，必须属于当前 Forge 实例，跨 Forge 在请求前拒绝。
客户端原有仓库作用域不覆盖显式传入的源仓或目标仓；Repository.id() 提供统一身份引用。
`prs_for_branch(branch, source_repository=...)` 在当前目标仓查询并匹配源仓，省略源仓时取当前仓。
工具包不自动选择父仓，调用方负责目标分支选择。
GitHub 分支查询在 owner:branch 服务端过滤后仍核对完整 repo；GitLab 核对 source_project_id。
列表分页完成后才返回，不能把其他 fork 的同名分支当作结果。

GitHub 创建请求发送到目标仓，指定 head owner 与 head_repo；GitLab 请求发送到源项目，明确
指定 target_project_id。平台数值 ID 留在 adapter 中，公共仓库身份仍是 Forge 与 path。
PullRequest 的 source_repository / target_repository 保存可确认的路径，缺失保留为空。

Python `git_state.fetch` 和 `ls_remote_tips` 接受显式 remote 名或 URL，默认 origin；选择远端、
本地 ref 命名以及 push / fetch 的协作策略属于调用方。

## Forge 仓库分页

TypeScript `ForgeReader.listRepositories({ pageSize?, cursor? })` 和 Python
`Forge.list_repositories(page_size=..., cursor=...)` 返回 `Page<RepositorySnapshot>` / `Page[Repository]`。
默认每页 50 项，允许 1..100 项。身份来自 common，分页契约属于 repocli。
范围是当前凭据拥有或参与的仓库，不能据此推断账号曾贡献过的全部目标仓库。
GitHub 使用 `/user/repos`，GitLab 使用 `/projects?membership=true`；不扫描本地 checkout。

每次调用一次 HTTP 请求，返回 items 和可选 nextCursor / next_cursor。当前 adapter 使用平台页号分页；
不透明 continuation 封装页号及查询条件，**不是 last_id/keyset 游标，也不提供原子快照保证**。
TS 游标绑定 provider、forge、endpoint、凭据摘要、查询及 pageSize；Python 绑定 provider、host、操作及 page_size，
由调用方保证凭据相同。省略页大小时沿用游标值。满页可能保守产生下一页，最终页可以为空。
不跟随游标或分页头中的任意 URL，避免向其它地址发送凭据。凭据轮换后 TS 需要重新分页。

```typescript
const first = await forge.listRepositories({ pageSize: 50 });
if (first.nextCursor) {
  const next = await forge.listRepositories({ cursor: first.nextCursor });
}
```

`iterateRepositories(reader, query?)` 使用 `@compforge/ts-stdx/iter` 的 loadingIterator。
构造不请求，当前页消费完才加载下一页，break 不预取，错误直接抛出。每个 iterator 独立、单次消费，
不自动重试、不缓存跨轮次清单、不保存业务检查点。清单快照应在遍历成功后整体替换；已 yield 的数据不会撤回。
需要持久化分页进度时使用单页 API，先持久消费该页再保存 continuation。

成员关系、仓库新增或重命名可能导致翻页重复或遗漏。调用方按 common 身份去重，不能仅因清单缺席判定删除。
无法读取或不兼容的端点报错，不自动回退成本地仓库集合或平台专有 API，也不编造 total。

## TypeScript Forge 读取

`createForgeReader` 显式接收 `{forge, host, provider, token}`。forge 是 common 身份名，host 是网络地址；
配置不含作者筛选，作者属于每次查询。库不读取应用环境变量、不选择账号、不持久化应用状态。

| 操作 | 结果与语义 |
|---|---|
| `listPullRequests(query)` | 单页 `Page<PullRequestKey>`；显式 account 或 repository scope |
| `iteratePullRequests(reader, query)` | 惰性逐项遍历，沿用单页查询；有行级 issues 时拒绝，不宣称清单完整 |
| `getPullRequest(key)` | common `PullRequestSnapshot`，仅基本事实，不请求评审 API |
| `getPullRequestReview(key)` | 独立 `PullRequestReviewObservation`；评审不可用则抛错 |
| `listPullRequestComments(key)` | 普通评论及嵌套评审回复 |
| `scanPullRequests(query)` | 有界持续扫描，返回 snapshots、检查点及覆盖状态 |

```typescript
const query = {
  scope: { kind: "account" as const, author: "octocat" },
  state: "all" as const,
  updatedAfter: "2026-10-01T00:00:00Z",
  updatedBefore: "2026-10-26T00:00:00Z",
  pageSize: 50,
};
const page = await forge.listPullRequests(query);
for (const key of page.items) {
  const snapshot = await forge.getPullRequest(key);
  // Persist common facts before committing the page continuation.
}
// Inspect page.issues; a partial page does not establish complete coverage.
```

account scope 按显式平台 username 跨可见仓库查询，不从 token 推断作者。
repository scope 为 `{kind: "repository", repository: RepositoryKey, author?: string}`，可查询仓内所有作者。
state 支持 open / closed / merged / all（默认 all），closed 不含 merged。
查询可带 updatedAfter / updatedBefore；游标不能跨 scope、作者、状态、时间范围或页大小复用。

GitHub 使用 Search API，支持 repository、author、状态和时间窗口；单个搜索窗口超过 1000 条或
incomplete_results 时抛 ForgeCoverageError，不能把截断结果当完整清单。GitLab 使用仓库或实例的
merge_requests API，作者通过 author_username 过滤。仓库查询直接使用给定 RepositoryKey；账号查询从
web_url 确认目标仓，支持 `/-/merge_requests/:iid` 和 `/merge_requests/:iid`，校验 origin、路径和编号。
单行异常保留其余有效 keys，并返回结构化 issues；全页传输或 envelope 错误直接抛出。
按页读取仍可能因并发变动漏项或重复，顺序 iterator 不会改变这个属性。

基本事实与源/目标分支来自 common，身份为目标 Repository + number；使用 pullRequestId / repositoryId
计算身份，结果可 JSON 或 structuredClone 往返。GitHub 保留已知 fork 源仓；GitLab 源项目与目标项目
相同时保留源仓，否则未知路径保持缺失。getPullRequest 不附评审时间或评审错误。

无冲突、平台 ready 与已 merged 是三类独立事实。GitHub 基本 REST 读取给出 conflictState 和已知 draft，
其它 mergeReadiness 来自显式的 getPullRequestReview；GitLab 基本 MR 的 detailed_merge_status 可提供
readiness。评审结果包括 key、reviewThreadState、reviewActivityFingerprint、observedAt，以及可用的 readiness。
指纹覆盖评审线程解决状态和完整评论活动，编辑、删除也会改变它；失败不能生成可信指纹。
评论排除 GitLab system notes。评审/comment/discussion 最多 20 页，超限抛错。

HTTP 使用 Node 原生 fetch 及其连接复用，可注入 fetch；单请求（含正文）默认超时 10 秒，响应上限 4 MiB，
由 timeoutMs / maxResponseBytes 调整。评论查询最多并行两个请求。401/403（排除限流）为 ForgeAuthError，
404 为 ForgeNotFound，传输错误为 ForgeError。ForgeRateLimitError 携带 retryAfterMs，同实例传输在冷却期
拒绝新请求，不自动重试。基础事实不会因“读取时顺带查询评审”而丢失；两类显式请求仍共享平台凭据和冷却。

### 持续扫描与覆盖

`scanPullRequests({scope, cursor?, initialSince?, tracked?})` 使用上面的通用查询和基本事实读取。
initialSince 仅决定首次历史回看（默认七天），首次另枚举可见 open 对象。tracked 是消费方持有的 keys，
独立复查用于确认其 merged / closed；清单缺席永远不构成终态证据。

扫描 cursor 是版本化检查点，包含窗口、缓冲页位置、失败对象重试队列与覆盖缺口；它与单页 cursor 不可互换。
枚举进度 enumeratedThrough 与无缺口的 scannedThrough 分开维护，都是私有字段。
调用方只保存完整 cursor，**可靠消费全部 items 后再提交**；同一检查点重放可重复返回事实。
账号、仓库、连接或凭据改变后要开始新扫描，恢复方式见 [0.8 迁移](migrations/forge-0.8.md)。

返回语义：

- `items`：本批成功读取的 common snapshots，可独立消费。
- `pending`：当前扫描轮次还有未执行工作，适合继续请求下一批；false 时可回到正常轮询间隔。
- `complete`：当前没有待执行工作，也没有保留的覆盖缺口。`pending=false, complete=false` 表示本轮已处理完，但仍存在错误待后续重试。
- `issues`：结构化 stage、kind、message，以及已知 key、scope、window、retryAfterMs。保留未恢复错误，不把失败当空清单。

失败详情读取会推进当前页位置，并将 key 放入持久重试队列；后续对象可继续返回。每批轮转重试最多 2 个，
最多发现 4 个窗口、读取 5 个新对象及 5 个 tracked 对象。重试、发现、tracked 各有独立 20 秒总期限，
单请求期限仍生效。队列最多 500 个失败对象，满时暂停新详情的接纳并报告覆盖问题，直到重试腾出容量；
已有失败仍重试，不静默丢弃对象。消费方根据 issues 记录日志并选择正常轮询或退避，库不启动后台任务。

为减少可变页号的遗漏，扫描每个时间窗口只取第一页：无法一次覆盖时按更新时间二分，边界重叠。
GitHub 搜索上限触发窗口细分。最小一秒窗口仍满 50 条或有后续页时记录 coverage gap；
不在该窗口内靠翻页宣称完整。失败窗口保留、其它窗口继续；同一状态的缺口合并为保守的时间区间，
下轮可能重读其中成功区域，以避免无限积累失败窗口。正常增量保留一分钟重叠。

这是快照扫描，不是事件日志；无法保证捕获轮询间的中间变化、已删除/失权对象或超出重叠范围的迟到搜索索引。
repocli 管请求与扫描覆盖，消费方管轮询、持久化、基线对比、去重和业务 Input 接纳。

### 仓库范围的工作流契约

`RepositoryForge` 是绑定单仓库的读写接口契约，参数中的 number 均属于绑定的仓库；TS 目前不提供它的原生写实现。
ForgeReader 是独立的跨仓只读客户端，不能互相替代。
`ForgeComment` 表达内容事实；`RepositoryForgeComment` 在此基础上附可选 `target: ForgeCommentTarget`。
replyRef / resolveRef 是写 adapter 提供的不透明句柄，缺失表示没有该能力；调用方不从普通 comment.id 推导它们。
评论 resolution 可为 unsupported / unresolved / resolved，缺失表示未观察。

升级映射与旧检查点处理见 [TypeScript 0.8 / Python 0.10 迁移](migrations/forge-0.8.md)。
