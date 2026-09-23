# Execution logs

Execution recording belongs to the shared CLI boundary described in the
[project kernel](kernel.md). This document specifies log storage and data handling.

Successful help/version invocations skip file logging and retention cleanup.
Analysis execution and invalid invocations retain the recording lifecycle below.

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

Every diff invocation that reaches analysis appends one JSON object to
`~/.repocli/logs/diff-YYYY-MM-DD.jsonl`, regardless of text or JSON output.
Completed, partial, empty, failed, and timed-out analyses are recorded. This
file shares the 30-day retention, append-only writes, permissions and warning
behavior of execution logs.

Each schema-2 record includes `version`, `from` (resolved base commit), `to`
(resolved head commit, or the input kind for mutable inputs), `testFiles`,
`checkout`, `input`, `snapshot`, `impactMode`, `testDirs`, `changedFiles`,
`patchFile` when applicable, `timeout`, `scope`, `complete`, and `diagnostics`.
`time` and `runId` link the record to its command log. `status` describes analysis
completion: `completed`, `deadline_exceeded`, `canceled`, or `failed`. On failure,
result fields that were not resolved remain empty; the command log holds the error.
Empty lists are JSON arrays.

`timeline.totalMs` measures analysis time. Each step has a name, its end offset
`atMs`, its `durationMs`, and optional count/status fields. `analysis.impact`
contains `workset.before`, `workset.after`, `query.before`, and `query.after`, so
nested durations must not be summed. A failing stage is still recorded when it
returns. The timeline uses [go-stdx's timeline](https://github.com/compforge/go-stdx/tree/main/timeline)
for collection; JSON field names and units belong to repocli.

For comparison across versions, rerun diff in the recorded checkout using `from`
as `--base`, commit `to` as `--head`, and the recorded query options. Working-tree,
index and patch runs require the original contents or patch; `snapshot` verifies
identity but is not a backup. Relative patch paths resolve from the invocation cwd
recorded in the command log. Stdin patches are not copied into history.

Argument failures before analysis have no diff history entry; their errors remain
in the execution log. Analysis is recorded before writing stdout, so an output
failure can still have a completed analysis entry. Consult the matching command's
exit status.
