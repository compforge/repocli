# Execution logs

Execution recording belongs to the shared CLI boundary described in the
[project kernel](kernel.md). This document specifies log storage and data handling.

Readable `slog` records are prefixed with `[repocli]` and appended to
`~/.repocli/logs/YYYY-MM-DD.log` using the local date. A shared `run_id` connects
invocation arguments, working directory, version, stdout/stderr copies, elapsed
time, error details and exit status. Output is stored in escaped `data` fields;
its original destinations and exit behavior remain unchanged. An interrupted
process may leave a start record without a terminal record.

Concurrent invocations append to the same daily file. Each invocation removes
dated regular log files older than the preceding 29 days; unrelated files,
directories and symlinks remain untouched. New directories/files use permissions
0700/0600. Setup or write failures warn once on the original stderr without failing
the command or recursively logging that warning.

The logger retains invocation arguments and full command output, including input
text quoted by errors. It does not separately dump source, patches or environment
variables. No log records are mixed into JSON stdout.

## Diff history

Every completed diff analysis also appends one JSON object to
`~/.repocli/logs/diff-YYYY-MM-DD.jsonl`, regardless of text or JSON output. Partial
and empty results are recorded too. This file shares the 30-day retention,
append-only writes, permissions and warning behavior of execution logs.

Each schema-1 record includes `version`, `from` (resolved base commit), `to`
(resolved head commit, or the input kind for mutable inputs), `testFiles`,
`checkout`, `input`, `snapshot`, `impactMode`, `testDirs`, `testPatterns`, `changedFiles`,
`patchFile` when applicable, `timeout`, `scope`, `complete`, and `diagnostics`.
`time` and `runId` link the result to its command log. Empty lists are JSON arrays.
An empty `testPatterns` means the recorded version's language defaults; older records
without this field also use defaults.

For comparison across versions, rerun diff in the recorded checkout using `from`
as `--base`, commit `to` as `--head`, and the recorded query options. Working-tree,
index and patch runs require the original contents or patch; `snapshot` verifies
identity but is not a backup. Relative patch paths resolve from the invocation cwd
recorded in the command log. Stdin patches are not copied into history.

Analysis or argument failures have no result to append; their errors remain in the
execution log. A completed analysis is recorded before writing stdout, so an output
failure can still have a history entry. Consult the matching command's exit status.
