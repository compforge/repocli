# TypeScript 仓库工具包

## 项目定位与边界

`@compforge/repocli` 直接完成 tree / inspect 与文件归属查询，只调用 Git，不启动 repocli CLI。
Repository、Component、Product 复用 harness-common；执行策略、日志与结果持久化属于调用方。
共享契约见 [仓库结构识别](../../docs/repository.md)。

## 代码地图

- `src/index.ts`、`model.ts`：公共入口、结果与 owner 查询。
- `src/inspect.ts`：输入版本、统一时限及完整性判定。
- `src/git.ts`：有界 Git 目录与必要元数据读取。
- `src/layout.ts`：配置、组件发现、语言和工具证据。
- `src/language.ts`、`languages.json`：生成的文件名识别目录，不加载 AST。
- `tests/` 与 `../../conformance/inspect/`：真实 Git 回归、Go/TS 共享语料。

## 关键约定

保持 Node ESM 可用；在本目录运行 `make lint`、`make test`、`make build`。
不手改生成目录；在仓库根运行 `make generate-languages`，并运行 Go 与 TS 两侧检查。
包版本由 package.json 独立维护；代码变化同时遵守仓库根 VERSION 约定。

仓库操作契约见 [operations](../../docs/operations.md)。分析与写操作分别显式调用，不能让分析触发写入。
