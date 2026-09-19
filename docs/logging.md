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
