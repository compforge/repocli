# Execution logs

Execution records help find the slowest cases and replay them to compare performance.
The shared CLI boundary owns recording; see the [project kernel](kernel.md).
Successful help/version invocations skip file logging and retention cleanup.

Readable `slog` records are prefixed with `[repocli]` and appended to
`~/.repocli/logs/YYYY-MM-DD.log` using the local date. Each invocation records:

- Start time, `run_id`, tool version, working directory, and arguments. `args` is
  a JSON array encoded as a text-log string, preserving argument boundaries.
- Completion or failure, command path, elapsed milliseconds, and exit code.

Command reports and errors go to their original output streams. Their contents,
including diagnostics and selected tests, are not copied into execution logs.
Analysis-stage errors are retained in the diff timeline described below. An interrupted
process may leave a start record without a terminal record.

Concurrent invocations append to the same daily file. Each invocation removes
dated regular log files older than the preceding 29 days; unrelated files,
directories and symlinks remain untouched. New directories/files use permissions
0700/0600. Setup or write failures warn once on the original stderr without failing
the command or recursively logging that warning.

## Diff history

Every diff invocation that reaches analysis appends one JSON object to
`~/.repocli/logs/diff-YYYY-MM-DD.jsonl`, regardless of text or JSON output.
Completed, partial, empty, failed, and timed-out analyses are recorded. This
file shares the retention, append-only writes, permissions and warning behavior
of execution logs.

Each schema-5 record contains the native timeline and replay inputs:

- `time`, `runId`, and `version` link the analysis to its command log.
- `checkout`, `from` (resolved base commit), `to` (resolved head commit or mutable
  input kind), `input`, and `snapshot` identify the comparison.
- `testDirs`, `changedFiles`, `patchFile` when applicable, and `timeout` preserve
  the query options. Empty lists are JSON arrays.
- `status` is `completed`, `deadline_exceeded`, `canceled`, or `failed`.

Analysis report fields (`scope`, `complete`, `diagnostics`, and `testFiles`) are
not duplicated in history. Schema 5 replaces schema 4's compact timing steps
with the native snapshot.

`timeline` stores the native [go-stdx Snapshot](https://github.com/compforge/go-stdx/tree/main/timeline).
Its operation ID matches `runId`. Stages retain IDs, parent IDs, start/end times,
status, errors, and count fields. `analysis.impact` is the parent of
`workset.before`, `workset.after`, `query.before`, and `query.after`; overlapping
or nested durations must not be summed. Stages are ordered by start time, then ID.
Times use RFC 3339 and `elapsed_ns` uses nanoseconds.

The CLI records into private memory and collects after analysis, including after
cancellation. The operation and active stage retain the analysis result;
`collection.local_flushed` and `collection.store_read` describe collection success
separately. A collection failure warns without changing the analysis result.
Snapshots can be decoded and queried using go-stdx without a repocli timing DTO.

## Find and retry slow cases

Sort completed command records by `elapsed_ms`, or diff history by
`timeline.elapsed_ns`, and inspect the slowest cases first. Use `runId` to find the
matching command's working directory and exact arguments. Decode the text-log
string and its JSON array, then invoke repocli with those arguments from that directory.

For comparisons across versions, use the recorded `from` as `--base` and commit
`to` as `--head`, retaining the query options. Working-tree, index and patch runs
require the original contents or patch; `snapshot` verifies identity but is not
a backup. Relative paths resolve from the invocation working directory. Stdin
patches are not copied into history. Compare timings only after checking that
the retries used the same input.

Argument failures before analysis have only a command record. On analysis
failure, unresolved comparison fields remain empty; use the original arguments
for replay. Analysis is recorded before writing stdout, so an output failure can
still have a completed analysis entry. Consult the matching command's exit code.
