# repocli

## 项目定位与边界

repocli 是面向 Git 仓库的独立命令行工具。项目共享概念、主流程和模块边界以
[docs/kernel.md](docs/kernel.md) 为准；专题文档描述各能力的模型与契约。

## 代码地图与核心模块

```text
main.go           # 进程入口、信号与退出码
cmd/              # Cobra 根命令、子命令、参数与输出适配
VERSION           # 版本号，由入口嵌入二进制
internal/
  analysis/       # 仓库快照、变更分析与组件归属的编排
  git/            # 只读 Git 基线与工作区快照
  diff/           # patch 解析、变更行与内存中的 postimage 重建
  codegraph/      # 适配共享 CodeGraph，补充仓库上下文并执行带证据的局部关系查询
    internal/syntax/ # repocli 专有的 import、导出和配置上下文事实
  impact/         # diff 起点、测试候选、影响筛选与缺口归属
  project/        # common 身份、组件布局与语言发现、产品关联和文件归属
docs/kernel.md    # 项目共享概念、主流程与边界
docs/codegraph.md # 通用图概念、构图流程与边界
docs/diff.md      # diff 消费图的流程和关键取舍
```

## 关键约定

1. 遵守 [项目内核](docs/kernel.md) 的只读、事实与推断分离、版本一致性和依赖方向；修改共享边界时同步该文档，功能细节留在专题文档。
2. 公共仓内容保持平台中立，不包含内部地址、个人机器路径、凭据或公司专属逻辑。
3. 修改代码文件时，必须在同一 PR 中同步 bump 根目录 `VERSION`；默认升 patch，新增功能或不兼容变更按 SemVer 选择 minor/major。仅文档改动无需升级。`VERSION` 是二进制版本的唯一来源，发布 tag 必须为 `v<VERSION>`。

开发验证入口：`make fix`、`make lint`、`make test`、`make build`。关键行为用隔离的临时仓库和
多语言源码样例验证，不能执行被分析项目的测试来替代分析器自身的回归。

## References

- [docs/kernel.md](docs/kernel.md)：项目内核、跨命令概念与关键边界。
- [README.md](README.md)：使用方式、输出语义、语言范围和限制。
- [docs/codegraph.md](docs/codegraph.md)：局部图的概念、流程、关系证据与支持边界。
- [docs/diff.md](docs/diff.md)：diff 分析模型、快照、影响传播和缺口边界。
- [docs/snapshot.md](docs/snapshot.md)：内容摘要、输入范围与完整性契约。
