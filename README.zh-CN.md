# repocli

[English](README.md)

面向 Git 仓库的 CLI 工具，供开发者、脚本和 coding agent 使用。
repocli 围绕源码文件、组件和语言等仓库上下文提供工具能力，`diff` 是当前首个可用命令。

## 安装

需要 Go 1.25+ 和 Git，从源码安装：

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

## 使用

`diff` 输出改动的源码与符号、可能受影响的测试文件，以及组件上下文，
支持 Go、Python、JavaScript 和 TypeScript。

```sh
repocli diff --repo /path/to/repo --base main --test-dir tests --json
```

省略 `--json` 输出可读文本。多个测试目录可重复传入 `--test-dir`；测试与源码同目录时，
使用 `--test-dir .`。不传测试目录则只报告变更。`--base` 默认为 `HEAD`，比较该提交与当前工作区。

测试影响范围是静态估计，结果由调用方自行消费，`diff` 不执行项目命令。
patch 输入、输出字段和分析限制见 [diff 使用说明（英文）](docs/diff-usage.md)。

`diff` 默认追加日志到 `~/.repocli/logs/YYYY-MM-DD.log`，保留今天及之前 29 天。
每次调用有独立 `run_id`，记录耗时、比较范围、结果数量及诊断，并复制 stdout/stderr，无需调用方传参。
可用 `--no-log` 关闭；help/version 不写日志，stdout 保持原有结果。
