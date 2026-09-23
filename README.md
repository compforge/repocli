# repocli

[中文](README.zh-CN.md)

A CLI for Git repositories, for developers, scripts, and coding agents.
repocli provides repository tools with context about source files, components,
and languages. It reports changes and identifies repository contents.

## Installation

Requires Go 1.26+ and Git. Install from source:

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
and component context. Declaration discovery follows CodeGraph language capabilities;
dependency-aware test selection supports Go, Python, JavaScript, and TypeScript.

```sh
repocli diff --repo /path/to/repo --base main --test-dir tests --json
```

Omit `--json` for readable text. Repeat `--test-dir` for multiple directories;
use `--test-dir .` for tests alongside source files. Without it, only changes are reported.
`--base` defaults to `HEAD` and compares that commit with the working tree.

Test impact is best effort: known-target inferred edges participate in recommendations,
with confidence retained on explanation edges; unknown targets are omitted. An empty
test list does not prove that no tests are affected. Callers decide how to use the result. `diff`
does not run project commands. See [diff usage](docs/diff-usage.md) for patch input,
output fields, and analysis limits.

`snapshot` identifies repository contents without running change or impact analysis:

```sh
repocli snapshot --json
repocli snapshot --staged --json
repocli snapshot --head HEAD --json
```

Its digest uses the same rules as `diff`; check `complete` before comparing it.
See [snapshot usage](docs/snapshot.md) for scope and limitations.

Analysis commands and invalid invocations automatically log invocations, stdout/stderr, errors and exit status
to `~/.repocli/logs/YYYY-MM-DD.log`, keeping today and the previous 29 days.
Each recorded run has a `run_id`; output destinations stay unchanged.
Successful help/version commands do not initialize or write log files.
Diff analyses also append comparison inputs, test lists, and stage timings to `diff-YYYY-MM-DD.jsonl`
in the same directory. See [execution logs](docs/logging.md).
