# repocli

[English](README.md)

面向 Git Repository 的解析与分析工具包，同时提供 Go API 和 CLI，供开发者、脚本与 coding agent 使用。
工具包负责仓库组织、内容身份、变更分析与代码图；CLI 将这些能力映射成命令，并提供本地代码图浏览。

测试推荐采用 best-effort 方式：有目标的推测关系参与推荐，解释路径保留置信度；未知目标不记录。
空测试列表不证明没有测试受影响。

| 命令 | 用途 |
|---|---|
| `inspect` | 查询组件目录、语言和包工具证据 |
| `snapshot` | 判断仓库内容身份及捕获完整性 |
| `diff` | 分析改动及可能受影响的文件、测试 |
| `view` | 交互浏览代码图和对应源码 |

## 安装

需要 Go 1.26+ 和 Git，从源码安装：

```sh
git clone https://github.com/compforge/repocli.git
cd repocli
make install
```

标签 [Release](https://github.com/compforge/repocli/releases) 提供 macOS/Linux 压缩包及校验和，
用 `repocli version` 或 `repocli --version` 查看已安装版本，`repocli version --json` 输出 JSON。
源码构建与发布版都使用编译时嵌入的根目录 `VERSION`，运行时不依赖仓库文件。

`make install` 使用 `go install`，安装到 `GOBIN`；未设置时安装到 `$(go env GOPATH)/bin`。
确保该目录在 `PATH` 中。仅构建可用 `make build`，产物位于 `bin/repocli`。

## 浏览代码图

```sh
repocli view
repocli view --repo /path/to/repo --addr 127.0.0.1:5484
```

打开命令打印的本地 URL，可查看 Document 概览、搜索符号、筛选关系类型与 confidence，
并查看对应快照的源码和诊断。修改代码后点击 **Refresh snapshot** 重新捕获。
Cytoscape.js 和页面资源内嵌在 Go 二进制中，运行时无需 Node.js、CDN 或数据库。
范围与限制见 [代码图浏览](docs/view.md)。

## Go API

导入 `github.com/compforge/repocli`，在调用方进程内分析仓库：

```go
report, err := repocli.Inspect(ctx, repocli.InputRequest{Repository: repoPath})
if err != nil {
    return err
}
if !report.Complete {
    return fmt.Errorf("仓库结构观察不完整：%v", report.Diagnostics)
}
component := report.Owner("server/main.go") // 无所属组件时返回 nil
```

`Inspect` 提供仓库组织，`Snapshot` 提供内容身份，`Diff` 提供变更及影响证据，`Graph` 提供节点、
关系和对应版本的源码。调用方通过 context 设置超时与取消，并决定如何处理不完整结果。
库可以调用 Git，但不会启动 repocli 进程、执行目标项目命令或写入 CLI 日志与历史。
边界见 [工具包内核](docs/kernel.md)。

Go module 保留在仓库根目录，二进制入口是 `./cmd/repocli`。

## 使用

`diff` 输出改动的源码与符号、可能受影响的文件，以及组件上下文。声明发现遵循 CodeGraph
的语言能力；依赖感知的测试选择支持 Go、Python、JavaScript 和 TypeScript。

```sh
repocli diff --repo /path/to/repo --base main --test-dir tests --json
```

省略 `--json` 输出可读文本。多个测试目录可重复传入 `--test-dir`；测试与源码同目录时，
使用 `--test-dir .`。不传测试目录则以仓库内受支持的源码作为候选入口，仍受构图预算约束。`--base` 默认为 `HEAD`，比较该提交与当前工作区。

影响分析自动选择符号、文件或包起点；每个文件报告起点、路径置信度、依赖距离和证据。
局部提取缺口保留为 observation，不使全部候选降级。影响范围是静态估计，结果由调用方自行消费，`diff` 不执行项目命令。
patch 输入、输出字段和分析限制见 [diff 使用说明（英文）](docs/diff-usage.md)。

`inspect` 查询仓库结构，不读取源码内容或计算全仓摘要：

```sh
repocli inspect --json
repocli inspect --staged --json
repocli inspect --head HEAD --json
```

输出 Repository 身份、组件根目录、语言和包工具证据。识别规则与限制见 [仓库结构识别](docs/repository.md)。

`snapshot` 读取仓库内容摘要，无需运行变更或测试影响分析：

```sh
repocli snapshot --json
repocli snapshot --staged --json
repocli snapshot --head HEAD --json
```

snapshot schema 2 只报告内容身份与捕获完整性；原先读取组件字段的调用方改用 `inspect`。
摘要规则与 `diff` 一致；比较前须检查 `complete`。
详见 [snapshot 使用说明（英文）](docs/snapshot.md)。

分析命令和无效调用默认记录工作目录、完整参数、版本、耗时和退出码，日志位于
`~/.repocli/logs/YYYY-MM-DD.log`，保留今天及之前 29 天。
每次记录有独立 `run_id`；参数保留为 JSON 数组，便于准确重试。
成功的 help/version 调用不初始化日志。命令报告与错误仍输出到原位置，内容不复制到日志。
diff 分析另追加到同目录的 `diff-YYYY-MM-DD.jsonl`，保留版本、比较输入、查询参数和阶段耗时。
按耗时找出最慢 case，以原参数重试，并核对快照摘要后比较；工作区、index 和 patch 重试须保留原始输入。
详见 [日志说明（英文）](docs/logging.md)。
