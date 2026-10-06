# repocli

## 项目定位与边界

repocli 是面向 Git Repository 的解析与分析工具包，CLI 是工具包的命令行适配。
公共 Go API 位于 `toolkit/go` 的 module 根包；项目共享概念、主流程和模块边界以
[docs/kernel.md](docs/kernel.md) 为准；专题文档描述各能力的模型与契约。

## 代码地图与核心模块

```text
repocli/
├── toolkit/
│   ├── go/                 # 独立 Go module；公共 API 与内部分析实现
│   │   ├── repocli.go      # Inspect / Snapshot / Diff / Graph
│   │   └── internal/      # analysis、git、project、diff、impact、codegraph
│   └── typescript/         # 原生 Node 工具包；inspect / owner
├── apps/cli/               # 独立 Go module；工具包的应用消费方
│   ├── cmd/repocli/        # 二进制入口、信号与退出码
│   └── internal/           # cli 参数/展示/执行记录，viewer 服务与页面
├── conformance/inspect/    # 多语言共享布局、输入版本及语言识别语料
├── docs/                   # 共享模型与能力设计
├── go.work                 # 本仓 Go 模块联合开发
├── VERSION                 # 仓库与 CLI 发布版本
└── Makefile                # 汇总各 Component 的验证与构建
```

## 关键约定

1. 遵守 [项目内核](docs/kernel.md) 的只读、事实与推断分离、版本一致性和依赖方向；修改共享边界时同步该文档，功能细节留在专题文档。
2. 公共仓内容保持平台中立，不包含内部地址、个人机器路径、凭据或公司专属逻辑。
3. **工具包优先**：CLI 与 viewer 通过 Go 工具包公共 API 获取分析结果；工具包不依赖 Cobra、HTTP、CLI 日志或调用方的验证策略。公开类型与方法属于兼容契约，内部算法保持私有。
4. **按语言组织实现**：`toolkit/go` 与 `toolkit/typescript` 是可直接消费的实现；`apps/cli` 是应用，Go 模块用各自的 internal 隔离私有实现。TypeScript 当前提供 inspect / owner。它直接完成仓库解析，与 Go 共享语义和契约用例，不包装 repocli 子进程。其它语言按同一原则按需加入，不预建空 SDK。
5. 修改代码文件时，必须在同一 PR 中同步 bump 根目录 `VERSION`；默认升 patch，新增功能或不兼容变更按 SemVer 选择 minor/major。仅文档改动无需升级。源码构建从 `VERSION` 注入 CLI 版本；仓库发布 tag 为 `v<VERSION>`，Go 子模块 tag 与 npm 版本规则见 [发布契约](docs/release.md)。

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

根 `make lint/test/check/build` 汇总各 Component；也可在 `toolkit/go`、`toolkit/typescript`、`apps/cli` 独立执行。根 `make install` 只安装 CLI。TS 验证需先安装该包依赖。

- [发布契约](docs/release.md)：Go 子模块与 CLI/npm 版本、发布顺序和构建。
