# 仓库结构识别

Go API `repocli.Inspect` 与命令 `repocli inspect` 报告选定版本的 Repository、Component、语言和包工具证据。身份类型复用
quality-harness 的 Go common；布局发现、文件归属和配置解释由 repocli 负责。

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

`project.Load` 是组件、语言和包工具判定的共同入口。inspect 提供轻量文件目录和必要配置；diff/view
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

- `pyproject.toml`、`go.mod`、`package.json`、`setup.py` 的存在提供组件根候选，不要求先解析
  项目声明，也不执行文件。Makefile、requirements 和
  tsconfig 单独出现不创建额外组件。未发现清单时回退为一个根组件。
- 仓库根组件可与子组件并存；已发现的非根组件停止嵌套发现。依赖、生成物、隐藏目录和 testdata
  不参与发现；Git 捕获边界先于组件发现生效。
- 文件归属于最长匹配的组件根目录。仅有子组件的仓库中，根目录共享文件可以没有归属。
- 语言可显式配置；自动识别按 Python、Go、Node 的清单优先级选择。Node 根据包中的 TypeScript
  线索或 tsconfig 区分 TS/JS；纯源码布局根据扩展名识别，多种语言为 mixed，未知时省略。
- common 的 Ecosystem 从语言派生为 python、go 或 node，不单独持久化；未知或 mixed 没有映射。

代码图中的 manifest 分类、项目名、版本号和模块声明来自 CodeGraph；组件识别依据仓库标记和
显式配置，命名、目录排除和嵌套规则由 repocli 决定。比如 `testdata/corpus/go.mod` 可以保留为 manifest 图事实，却不会自动成为 Component。
只含工具配置或解析不完整的 manifest 仍可作为目录边界候选；这不声称它是可构建的独立项目。
当前组件发现策略保留这一宽松规则，明确的项目边界通过版本化配置声明。

可用版本化配置覆盖身份和完整组件目录：

```json
{
  "repository": {"forge": {"name": "github"}, "path": "example/mono"},
  "components": [
    {"name": "api", "root": "server", "language": "python", "products": [{"name": "example-product"}]},
    {"name": "client", "root": "web", "language": "typescript"}
  ]
}
```

Product 关联只来自显式声明。语言、目录和包工具不参与 Component 身份，也不意味着组件间没有依赖。

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
