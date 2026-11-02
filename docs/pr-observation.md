# PR 持续观察

TypeScript `PullRequestObserver` 对外提供 `getPullRequest(key)`，返回 PR 身份、生命周期状态和观察时间。
业务调用方不指定缓存来源、刷新步骤或 tracked 集合。repocli 负责本地复用、远端读取、持久化、
周期更新、失败恢复和 merged 终态收敛。ForgeReader 保留原始平台读取能力，评论与评审继续独立查询。

## 使用与生命周期

```typescript
import { PullRequestObserver, createForgeReader } from "@compforge/repocli";
import { homedir } from "node:os";
import { join } from "node:path";

const reader = createForgeReader(config);
const prs = new PullRequestObserver(reader, {
  stateDir: join(homedir(), ".repocli"),
  connection: "github-personal-access-v1",
});
try {
  const status = await prs.getPullRequest({
    repository: { forge: { name: "github.com" }, path: "org/repo" }, number: 107,
  });
  console.log(status.state, status.observedAt);
} finally {
  await prs.close();
  reader.close?.();
}
```

创建 observer 显式启动维护循环，宿主在退出时调用 `close()`。一次 get 自动把 PR 纳入后续维护；
重启后恢复同一访问身份的本地记录。工具包要求显式 stateDir；命令行消费方可使用上面的默认目录。
本轮实现 TypeScript 工具包入口，Go CLI 不增加另一套 Forge adapter。
可配置 scopes 自动发现仓库或账号的 PR；initialSince 只控制首次历史回看，首次另枚举开放对象。
查询接受 AbortSignal；close 取消内部循环及协作式远端请求，不关闭调用方提供的共享 reader。

成功查询只返回满足新鲜度策略的生命周期事实。不能取得当前非终态状态时抛出
`PullRequestStatusUnavailable`，不会把旧观察当成最新结果。最近成功 snapshot 仍保存在本地。
`health` 给宿主提供后台轮询的最近完成时间、结构化 issues 和持久化错误，便于记录日志与告警。
周期刷新失败不使未处理的 Promise rejection 逃逸；下一轮继续恢复。持久化失败必须报告，不能当作日志写入失败忽略。

## 状态与目录

```text
<stateDir>/
├── repositories/
│   └── <repository-key>/
│       ├── repository.json
│       └── prs/<number>/
│           ├── observation.json
│           └── observation.json.lock/  # 仅持有刷新锁期间存在
└── scans/<scope-key>.json
```

repository-key 为 Forge 名称与完整目标仓库路径的摘要，repository.json 保留原始身份；
PR 身份为目标 Repository + number，与 checkout/worktree 无关。每个 PR 一个目录；空目录不表示观察成功。
observation.json 保存版本、访问配置摘要、最近成功 snapshot/observedAt、最近尝试时间和错误、
下次刷新时间、连续失败次数与发现范围关联。merged 事实保留在同一记录，不维护另一份终态标记文件。
scans 仅在启用范围发现后创建，保存绑定访问身份与查询范围的 cursor；跨仓库账号查询不归某个 Repository。

connection 是调用方提供的访问身份/配置修订号，不是凭据。更换凭据或权限后必须更换该值；
落盘只保存摘要。访问身份不匹配时不复用观察，一个 PR 的记录按最后访问身份替换；本版本不保留多账号缓存副本。
目录 0700、状态文件 0600；状态不参与 logs 的定期清理。存储根用于可信的本地私有状态。

## 刷新与失败恢复

默认 open 每 60 秒更新，closed 每 600 秒更新，因其可能重新打开；查询新鲜度默认 60 秒。
merged 是生命周期终态：可靠保存后停止常规状态刷新；其 observedAt 不会伪造更新。
标题、评论、评审等可变字段不受终态规则保证，使用 ForgeReader 的对应读取能力。
读取失败、404、权限错误和列表缺席均不推导终态，也不抹掉之前的成功事实。

失败按对象退避，默认 30 秒起、指数增长至一小时；尊重远端提供的更长 retryAfterMs。
后台每轮最多处理 5 个到期对象，按到期时间轮转；刷新失败持久化在该 PR 的记录中。
发现使用既有 scanner 的窗口、覆盖与有界补读能力，失败补读最多 500 个；满载只暂停发现详情的接纳，
并保留缓冲页位置，不阻断已知 PR 的刷新。连接级限流继续由共享 HTTP 客户端协调。
低层 scanner 使用 cursor v3，旧格式直接拒绝；低层 tracked 每次以当前传入集合为准，移除即退出刷新。

单请求默认 20 秒超时；外部 reader 应自行提供有界 I/O，并可通过 withSignal 协作取消。
等待锁最多 30 秒，锁每 10 秒续期、30 秒失效；统一使用 proper-lockfile 的目录锁与崩溃恢复机制，
新增该依赖是为跨进程协调，避免自建文件锁协议。相同状态目录的所有进程必须使用一致锁参数。
进程内或跨进程的查询均在锁内重新检查状态；先取得锁的刷新保存后，后续查询可直接复用。

状态通过临时文件、fsync、原子 rename 和目录 fsync 保存。失去锁或取消后不提交新状态。
先逐个保存 PR 观察，再保存扫描检查点；中断允许重复读取，不允许跳过未保存事实。
持久化完成只证明观察已保存，不表示任何调用方已完成业务消费。通知、业务基线和门禁仍由调用方拥有。

## 验证

`toolkit/typescript/tests/forge-observation.test.mjs` 使用隔离目录和真实子进程验证缓存重启、
merged 终态、closed 重开、失败退避、未知状态、并发合并、保存失败与检查点顺序、
饱和补读队列下的健康刷新和恢复。既有 ForgeReader 的读取、限流与扫描契约用例继续执行。
