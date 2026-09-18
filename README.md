# repocli

[中文](README.zh-CN.md)

A CLI for Git repositories, for developers, scripts, and coding agents.
repocli provides repository tools with context about source files, components,
and languages. `diff` is the first available command.

## Installation

Requires Go 1.25+ and Git. Build from source:

```sh
git clone https://github.com/compforge/repocli.git
cd repocli
make build
```

Tagged [releases](https://github.com/compforge/repocli/releases) provide macOS/Linux archives
and checksums. Use `repocli --version` to identify an installed build.

The binary is at `bin/repocli`; place it on your `PATH` to use it from any directory.

## Usage

`diff` reports changed source files and symbols, potentially affected test files,
and component context. It supports Go, Python, JavaScript, and TypeScript.

```sh
./bin/repocli diff --repo /path/to/repo --base main --test-dir tests --json
```

Omit `--json` for readable text. Repeat `--test-dir` for multiple directories;
use `--test-dir .` for tests alongside source files. Without it, only changes are reported.
`--base` defaults to `HEAD` and compares that commit with the working tree.

Test impact is a static estimate; callers decide how to use the result. `diff`
does not run project commands. See [diff usage](docs/diff-usage.md) for patch input,
output fields, and analysis limits.
