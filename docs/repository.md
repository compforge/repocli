# 仓库结构识别

Go API `repocli.Inspect`、TypeScript/Python API `inspect` 与命令 `repocli inspect` 报告选定版本的 Repository、Component、语言和包工具证据。身份类型复用
quality-harness 各语言的 common；布局发现、文件归属和配置解释由 repocli 负责。

## 基础内容视图

`repocli tree` / Go `Tree` / Python、TypeScript `tree` 返回目录、文件及已知文件角色。
它不读取 `.repocli.json`，因此配置无效或没有 Component 时仍可查询内容。与 inspect 一样，支持
工作区、index 和 commit，工作区包括未忽略的未跟踪文件；不递归进入独立仓库。

```sh
repocli tree --json
repocli tree --staged --json
repocli tree --head HEAD --json
```

结果包含 checkout、input、head、directories、files、complete 和 diagnostics；CLI JSON schema 为 1。
Directory 以 path 表达，根为 `.`；仅返回 Git 条目隐含的目录，不扫描忽略目录或空目录。
File 包含 path、kind、mode，以及可确定时的 oid。kind 为 regular、symlink 或 gitlink；role 可为
manifest、build_script 或 lockfile，未知角色省略。Manifest 另保留 path、ecosystem；文件角色与
Git kind 不混用，名为 go.mod 的符号链接不会被当作 Manifest。

工作区普通文件不计算 OID；gitlink 在已初始化时读取 HEAD，否则保留 index 引用。
index/commit 的 OID 来自对应版本。gitlink 本身不作为 Directory，子仓内容不进入父仓。
可变输入复读条目，不一致返回 tree_changed。这里的完整性只覆盖路径、类型与引用，内容身份使用 snapshot。
查询限制为 10,000 项，所有 Git 调用共用调用方时限；异常、超限、未合并 index 不返回虚假的空结果。

## inspect 与共享识别规则

```sh
repocli inspect --json
repocli inspect --staged --json
repocli inspect --head HEAD --json
```

默认枚举 Git 跟踪及未忽略的工作区文件；`--staged` 使用 index，`--head` 先解析为确定 commit。
inspect 只读取识别所需的配置内容（当前为根 `.repocli.json` 与各目录的 `package.json`），
其它普通文件仅提供路径、标记文件存在性和语言扩展名。它不读取源码内容、计算全仓摘要、解析 AST
或捕获子模块内容，也不执行项目代码、安装依赖。语言扩展名识别复用已有的语言能力目录，不触发解析。

Go 的 `project.Load` 与 TypeScript/Python 的内部 layout 实现遵守相同的组件、语言和包工具规则，
由 `conformance/inspect` 的共享用例验证。inspect 提供轻量文件目录和必要配置；diff/view
使用各自已经捕获的同版本材料。diff 的前后版本及 patch postimage 分别识别，不能借用工作区配置。
这些规则无需构建代码图；CodeGraph 在 diff/view 的代码事实分析阶段提供 manifest 声明、符号与关系。

JSON schema 1 包含 `repository`、`components`、`checkout`、`input`、可选 `head`、`complete` 和
`diagnostics`。没有 `snapshot` 摘要；它不能作为全仓验证结果复用的内容指纹。
`complete` 只描述路径和必要配置的观察是否完成，不证明项目可构建或自动发现的组件边界符合所有业务意图。
工作区和 index 会复读路径及必要配置；发现变化时返回 `inspection_changed` 且 `complete: false`。
这不能提供文件系统原子性，也不追踪无关源码内容的并发编辑。

文件目录最多 10,000 项，必要配置每份最多 2 MiB、合计最多 128 MiB。读取错误、未合并的 index、
超限或无效 `.repocli.json` 返回退出码 1，不伪造根组件。必要配置的符号链接不跟随；不读取嵌套仓库、
gitlink 内容。参数错误退出 2；正常报告（含观察到的并发变化）退出 0。
畸形 package.json 按既有最佳努力策略保留目录标记，但不提供 packageManager 证据。

`snapshot` schema 2 专注内容身份，不再携带仓库结构；查询旧组件字段的调用方改用 inspect。
view API 在顶层提供 `repository` 和 `components`，其 `snapshot` 仅描述内容身份；diff 保留组件影响结果。

## Repository、Component 与语言

Repository 身份由 `.repocli.json` 显式声明，否则来自 origin。无法确定时为 null，checkout 路径
单独报告。Component 的身份复用 common 类型，布局和工具证据由 repocli 持有。

- `pyproject.toml`、`go.mod`、`package.json`、`setup.py`、`Cargo.toml` 的存在提供组件根候选，不要求先解析
  项目声明，也不执行文件。Makefile、requirements 和
  tsconfig 单独出现不创建额外组件。无清单且无显式声明时，components 为空；纯源码工程可显式声明根组件。
- 仓库根组件可与子组件并存；已发现的非根组件停止嵌套发现。依赖、生成物、隐藏目录和 testdata
  不参与发现；Git 捕获边界先于组件发现生效。
- 文件归属于最长匹配的组件根目录。仅有子组件的仓库中，根目录共享文件可以没有归属。
- 语言可显式配置；自动识别按 Python、Go、Node、Rust 的清单优先级选择。Node 根据包中的 TypeScript
  线索或 tsconfig 区分 TS/JS；纯源码布局根据扩展名识别，多种语言为 mixed，未知时省略。
- common 的 Ecosystem 从语言派生为 python、go 或 node，不单独持久化；未知或 mixed 没有映射。

代码图中的 manifest 分类、项目名、版本号和模块声明来自 CodeGraph；组件识别依据仓库标记和
显式配置，命名、目录排除和嵌套规则由 repocli 决定。比如 `testdata/corpus/go.mod` 可以保留为 manifest 图事实，却不会自动成为 Component。
只含工具配置或解析不完整的 manifest 仍可作为目录边界候选；这不声称它是可构建的独立项目。
当前组件发现策略保留这一宽松规则，明确的项目边界通过版本化配置声明。

可用版本化配置覆盖身份和完整组件目录。`components: []` 显式声明无组件；字段省略或 null 才执行自动发现：

```json
{
  "repository": {"forge": {"name": "github"}, "path": "example/mono"},
  "components": [
    {"name": "api", "root": "server", "language": "python", "products": [{"name": "example-product"}]},
    {"name": "client", "root": "web", "language": "typescript"}
  ]
}
```

每个 Component 的 `manifests` 保留自身根目录内已观察到的 Manifest 路径；显式配置可以声明没有 Manifest 的组件。

Product 关联只来自显式声明。语言、目录和包工具不参与 Component 身份，也不意味着组件间没有依赖。

## Component 数量与归属

| 形态 | 组件根 | 目录与文件归属 |
|---|---|---|
| 无 Component | 空 | 保留内容，owner 为空 |
| 单个子目录 Component | app | app 内归属该组件，外部可无归属 |
| 单个根 Component | . | 根内普通目录也属于该组件 |
| 多个同级 Component | api、web | 分别归属，docs 等共享目录可无归属 |
| 根组件与子组件共存 | .、tools | 最深根优先，tools 内归子组件 |
| 无 Manifest 的显式组件 | 配置指定 | 依照声明归属，不伪造 Manifest |

这些形态由三语言共享的 `conformance/inspect/layouts.json` 验证；组件数量不改变 tree 的内容查询能力。

## 包工具证据

`packageTools` 是检测结果，每项包含 name、可选 version 和 evidence 路径。它描述当前捕获材料中的
工具线索；多条线索保留为多个候选，不指定执行工具。version 仅保留清单声明，不查询本机安装版本。

| 依据 | 工具 |
|---|---|
| go.mod | go |
| uv.lock / poetry.lock / Pipfile.lock | uv / poetry / pipenv |
| package.json 的 packageManager | npm / pnpm / yarn / bun，并保留声明版本 |
| package-lock.json 或 npm-shrinkwrap.json | npm |
| pnpm-lock.yaml / yarn.lock / bun.lock 或 bun.lockb | pnpm / yarn / bun |

只读取组件根目录的材料，不把父目录锁文件自动继承给子组件。workspace 成员关系、锁文件依赖图和
运行命令不在此检测范围。仅有 requirements 或 pyproject 不足以判定 pip 或其它具体管理工具。
未知工具保持缺省；畸形 package.json 不提供声明证据，但其独立锁文件仍可提供线索。

## TypeScript 调用契约

`inspect({ repository, staged, head, signal, timeoutMs })` 在 Node 进程内完成识别，返回 schema 1 对象。
`owner(report, path)` 返回最具体的 ComponentBinding，未归属时返回 undefined。结果与 common 身份字段
均按只读使用。Go 由 context 提供截止时间，TS 默认整个调用五秒，可通过 timeoutMs 调整并用 signal 取消。
读取失败或参数错误会抛出异常；部分结果仍由 complete 与 diagnostics 表达，退出码属于 CLI 适配层。

TS 与 Go 使用相同的路径、元数据容量及子仓库边界。TS 的单次 Git 输出另有 16 MiB 上限，超限拒绝；
读取 blob 前先批量检查不可变对象的大小，再按总预算读取。文件名识别目录来自固定依赖的生成产物，
Go 测试检查目录漂移，TS/Python 测试核对全部生成的路径探针。运行时无需 repocli 二进制或 Go 工具链。

## Python 调用契约

`inspect(repository, *, head=None, staged=False, timeout=5.0, cancel=None)` 提供同步调用，
返回 frozen dataclass `InspectReport`；字段使用 snake_case，集合使用 tuple。
`owner(report, path)` 为同步纯查询，接受仓库相对路径，未归属返回 None。取消使用 threading.Event；
整次调用共用秒级 timeout；Git 子进程在超时、取消或输出超限时终止并回收，文件系统读取与
Python 投影在操作间检查预算。超时、取消、无效配置和 I/O 错误抛出异常；并发变化仍以 complete/diagnostics 表达。
运行平台为 macOS/Linux，依赖 PATH 中的 Git，容量、版本选择和子仓库边界与 TS 一致。Git 负责
index、tree、ignore 和 revision 语义；Python 负责布局投影。目录和必要 blob 按批次读取，先核对
对象大小，再获取内容；不按源文件启动进程。容量约束作用于工具包接收的数据，不是 Git 进程内存上限。
Python 复用已发布 harness-common 的身份类型；ComponentBinding 承载当前布局元数据。
共享语料比较三种实现的语义投影，不要求 Python 内存对象复制 CLI JSON 字段命名。
