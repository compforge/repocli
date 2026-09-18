# repocli

[English](README.md)

面向 Git 仓库的 CLI 工具，供开发者、脚本和 coding agent 使用。
repocli 围绕源码文件、组件和语言等仓库上下文提供工具能力，`diff` 是当前首个可用命令。

## 安装

需要 Go 1.25+ 和 Git，从源码构建：

```sh
git clone https://github.com/compforge/repocli.git
cd repocli
make build
```

二进制位于 `bin/repocli`，放入 `PATH` 后即可在任意目录调用。

## 使用

`diff` 输出改动的源码与符号、可能受影响的测试文件，以及组件上下文，
支持 Go、Python、JavaScript 和 TypeScript。

```sh
./bin/repocli diff --repo /path/to/repo --base main --test-dir tests --json
```

省略 `--json` 输出可读文本。多个测试目录可重复传入 `--test-dir`；测试与源码同目录时，
使用 `--test-dir .`。不传测试目录则只报告变更。`--base` 默认为 `HEAD`，比较该提交与当前工作区。

测试影响范围是静态估计，结果由调用方自行消费，`diff` 不执行项目命令。
patch 输入、输出字段和分析限制见 [diff 使用说明（英文）](docs/diff-usage.md)。
