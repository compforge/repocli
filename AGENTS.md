# repocli

## 项目定位与边界

repocli 是面向 Git Repository 的解析与分析工具包，CLI 是工具包的命令行适配。
公共 Go API 位于 module 根包；项目共享概念、主流程和模块边界以
[docs/kernel.md](docs/kernel.md) 为准；专题文档描述各能力的模型与契约。

## 代码地图与核心模块

```text
repocli.go        # 公共 Go API：Inspect / Snapshot / Diff / Graph 与结果类型
version.go        # 工具包版本，嵌入唯一 VERSION 来源
typescript/      # 原生 Node 工具包：inspect / owner 与 common 身份类型
conformance/inspect/ # Go / TS 共享布局、输入版本与文件名识别语料
cmd/repocli/      # 二进制入口、信号与退出码
VERSION           # 工具包与 CLI 的发布版本
go.mod            # 根 Go module；调用方 import github.com/compforge/repocli
internal/
  cli/            # Cobra 参数、文本/JSON 输出、CLI 日志与执行历史
  viewer/         # 本地 HTTP 服务、内嵌页面；消费公共 GraphSnapshot
  analysis/       # 工具包内部：输入选择、组织/捕获/图与变更分析编排
  git/            # 只读 Git 文件目录、基线与工作区快照
  diff/           # patch 解析、变更行与内存中的 postimage 重建
  codegraph/      # 共享 CodeGraph 的仓库上下文、局部关系与证据适配
    internal/syntax/ # repocli 专有的 import、导出和配置上下文事实
  impact/         # diff 起点、测试候选、影响筛选与缺口归属
  project/        # common 身份、组件布局、语言与包工具证据、产品关联和文件归属
docs/kernel.md    # 工具包内核、公共边界及语言实现约定
docs/repository.md # 结构识别与文件归属契约
docs/codegraph.md # 局部构图与查询边界
docs/diff.md      # 变更分析与影响证据

```

## 关键约定

1. 遵守 [项目内核](docs/kernel.md) 的只读、事实与推断分离、版本一致性和依赖方向；修改共享边界时同步该文档，功能细节留在专题文档。
2. 公共仓内容保持平台中立，不包含内部地址、个人机器路径、凭据或公司专属逻辑。
3. **工具包优先**：CLI 与 viewer 通过根包公共 API 获取分析结果；工具包不依赖 Cobra、HTTP、CLI 日志或调用方的验证策略。公开类型与方法属于兼容契约，内部算法保持私有。
4. **按语言组织实现**：Go 保持根 module；原生 TypeScript 实现落在 `typescript/`，当前提供 inspect / owner。它直接完成仓库解析，与 Go 共享语义和契约用例，不包装 repocli 子进程。其它语言按同一原则按需加入，不预建空 SDK。
5. 修改代码文件时，必须在同一 PR 中同步 bump 根目录 `VERSION`；默认升 patch，新增功能或不兼容变更按 SemVer 选择 minor/major。仅文档改动无需升级。`VERSION` 是二进制版本的唯一来源，发布 tag 必须为 `v<VERSION>`。

开发验证入口：`make fix`、`make lint`、`make test`、`make build`。关键行为用隔离的临时仓库和
多语言源码样例验证，不能执行被分析项目的测试来替代分析器自身的回归。

## References

- [docs/repository.md](docs/repository.md)：inspect、共享仓库与组件识别。
- [docs/kernel.md](docs/kernel.md)：项目内核、跨命令概念与关键边界。
- [README.md](README.md)：使用方式、输出语义、语言范围和限制。
- [docs/codegraph.md](docs/codegraph.md)：局部图的概念、流程、关系证据与支持边界。
- [docs/diff.md](docs/diff.md)：diff 分析模型、快照、影响传播和缺口边界。
- [docs/snapshot.md](docs/snapshot.md)：内容摘要、输入范围与完整性契约。

- [docs/view.md](docs/view.md)：代码图浏览、快照一致性和展示边界。

Go Component 使用根 Makefile；TypeScript Component 使用 `typescript/Makefile`。`make check` 汇总两侧 lint/test，需先安装 TS 依赖。
