# repocli

[中文](README.zh-CN.md)

A CLI for Git repositories, for developers, scripts, and coding agents.
repocli provides repository tools with context about source files, components,
and languages. `diff` is the first available command.

## Installation

Requires Go 1.25+ and Git. Install from source:

```sh
git clone https://github.com/compforge/repocli.git
cd repocli
make install
```

Tagged [releases](https://github.com/compforge/repocli/releases) provide macOS/Linux archives
and checksums. Use `repocli version` or `repocli --version` to identify the installed
binary; `repocli version --json` produces structured output. Both source and release
builds embed the version from the repository’s `VERSION` file.

`make install` uses `go install`: the binary goes to `GOBIN`, or `$(go env GOPATH)/bin`
when `GOBIN` is unset. Ensure that directory is on `PATH`. Use `make build` to build
only, producing `bin/repocli`.

## Usage

`diff` reports changed source files and symbols, potentially affected test files,
and component context. It supports Go, Python, JavaScript, and TypeScript.

```sh
repocli diff --repo /path/to/repo --base main --test-dir tests --json
```

Omit `--json` for readable text. Repeat `--test-dir` for multiple directories;
use `--test-dir .` for tests alongside source files. Without it, only changes are reported.
`--base` defaults to `HEAD` and compares that commit with the working tree.

Test impact is a static estimate; callers decide how to use the result. `diff`
does not run project commands. See [diff usage](docs/diff-usage.md) for patch input,
output fields, and analysis limits.

`diff` automatically appends execution logs to `~/.repocli/logs/YYYY-MM-DD.log`,
keeping today and the previous 29 days. Each run has a `run_id`; logs include timing,
comparison inputs, result counts and diagnostics. Use `--no-log` to disable logging.
Help/version do not write logs; stdout remains the command result.
