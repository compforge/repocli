# 仓库上下文与命令准备

repocli 先理解选定内容版本所属的 Repository、Component 和文件归属，再执行命令的专属分析。
这份上下文与源码共享 snapshot 身份，供各命令复用。

## 通用 prepare

```text
选择工作区 / index / commit / patch 版本
  → Git 内容捕获或 patch 重建
  → CodeGraph manifest 事实
  → Repository 身份 + Component 布局、语言、包工具证据
  → snapshot 报告 / diff 分析 / view 构图
```

`analysis.Prepare` 返回捕获内容及其上下文；已有捕获结果通过同一个准备函数装配。
`diff` 分别准备前后版本，配置来自各自内容，patch 的 postimage 也遵循此规则。
`view` 每次刷新整体替换上下文、图与源码。组件发现使用全部捕获材料，早于图的文档数限制。

prepare 属于分析层，命令根据所选输入调用；help、version 等信息查询不访问目标仓库。
它不解析源码语法或执行目标项目代码、安装依赖。无效的 `.repocli.json` 使准备失败；捕获缺口仍由
快照诊断表达，`complete` 保持内容捕获完整性的含义。manifest 静态解析的缺口单独放在
`observations`，不会把已经完整捕获的内容误报为捕获失败。

可直接通过 `repocli snapshot --json` 查看 repository、components 和内容身份。`snapshot` 和
view API 的 snapshot 内使用组件布局条目；`diff` 将同一份元数据附加到组件影响结果。

## Repository、Component 与语言

Repository 身份由 `.repocli.json` 显式声明，否则来自 origin。无法确定时为 null，checkout 路径
单独报告。Component 的身份复用 common 类型，布局和工具证据由 repocli 持有。

- CodeGraph 的 manifest Document（`pyproject.toml`、`go.mod`、`package.json`）提供组件根的候选；
  repocli 另保留 `setup.py` 的旧式打包边界识别，不执行文件。Makefile、requirements 和
  tsconfig 单独出现不创建额外组件。未发现清单时回退为一个根组件。
- 仓库根组件可与子组件并存；已发现的非根组件停止嵌套发现。依赖、生成物、隐藏目录和 testdata
  不参与发现；Git 捕获边界先于组件发现生效。
- 文件归属于最长匹配的组件根目录。仅有子组件的仓库中，根目录共享文件可以没有归属。
- 语言可显式配置；自动识别按 Python、Go、Node 的清单优先级选择。Node 根据包中的 TypeScript
  线索或 tsconfig 区分 TS/JS；纯源码布局根据扩展名识别，多种语言为 mixed，未知时省略。
- common 的 Ecosystem 从语言派生为 python、go 或 node，不单独持久化；未知或 mixed 没有映射。

图中的 manifest 分类、项目名、版本号和模块声明来自 CodeGraph；组件命名、目录排除和嵌套规则由
repocli 决定。比如 `testdata/corpus/go.mod` 可以保留为 manifest 图事实，却不会自动成为 Component。
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
