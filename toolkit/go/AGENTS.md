# Go 仓库工具包

## 项目定位与边界

`github.com/compforge/repocli/toolkit/go` 是可独立导入的 Repository 解析与分析工具包。
分析入口在 `repocli.go`，显式 worktree 操作在 `worktree.go`，共享模型和职责见 [内核](../../docs/kernel.md)。

## 代码地图与核心模块

```text
repocli.go          # 分析 API 与结果类型
worktree.go         # 通用 Git worktree 查询与显式操作
internal/
├── analysis/      # 输入选择、识别、捕获、构图与变更分析编排
├── git/           # 只读 Git 材料与容量边界
├── project/       # common 身份、目录布局、语言、工具证据及归属
├── diff/          # patch、变更行与 postimage
├── impact/        # 影响策略、测试候选和缺口
└── codegraph/     # 共享代码事实及仓库解析上下文
```

## 关键约定

- 应用只能消费公共 API；不能为了 CLI 或测试放宽 internal 可见性。工具包不依赖 apps、Cobra、HTTP 展示或日志持久化。
- context 负责取消与时限；结果保持完整性和诊断，消费策略归调用方。Repository / Component / Product 复用 common。
- `make lint/test/build` 验证本模块。`../../conformance` 是仓库共享语料，测试需在完整仓库中运行。
- 文件名目录由 `make generate-languages` 更新，不能手改生成产物；TS/Python 运行时不依赖 Go。
