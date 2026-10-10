# Deadcode candidates

`repocli deadcode` lists declaration nodes with no incoming usage edge in the graph
of the selected repository snapshot.

```sh
repocli deadcode
repocli deadcode --head HEAD --json
repocli deadcode --staged --json
```

The Go toolkit exposes `AnalyzeDeadcode(ctx, DeadcodeRequest)`. The CLI adapts this
API and supports the usual repository, timeout, logging and text/JSON options.

## Rule and scope

All captured UTF-8 text documents enter one graph, including tests. The graph
uses the same snapshot and source bytes as repository capture, without the viewer's
document-count truncation or impact's workset selection. Capture and CodeGraph
resource budgets still apply; a build failure returns an error. For larger graphs,
raise `--max-nodes` and `--max-relations` explicitly (zero retains CodeGraph defaults):

```sh
repocli deadcode --head HEAD --max-nodes 250000 --max-relations 1000000 --json
```

Candidates are declared code nodes, excluding documents, directories, packages,
modules and namespaces. Structural edges (`declares`, `contains`, `encloses`,
`in_namespace`, `in_directory`, `occurs_in`) do not count as uses. Every other
incoming relation counts, including inferred edges and self-references. Fields and
local declarations participate when CodeGraph represents them as declaration nodes.

This initial rule does not model entrypoints, external callers, runtime dispatch,
initialization side effects or unreachable cycles. Public APIs and entrypoints can
appear. A candidate means no incoming usage edge was found; it is not a deletion
verdict. An empty result is not proof that the repository has no dead code.

## Candidate filters

Use `--exclude-tests` to omit candidates in test files, and
`--exclude-entrypoints` to omit nodes marked as native runtime entrypoints by
CodeGraph. Its current coverage is Go top-level `init` functions and `main`
functions in package `main`, with no type parameters, parameters or results:

```sh
repocli deadcode --head HEAD --exclude-tests --exclude-entrypoints --json
```

The Go request exposes `ExcludeTests` and `ExcludeEntrypoints`. Both default to
false, preserving the complete candidate list. These are report filters: all
captured text documents still enter the graph, so uses from tests and entrypoints
continue to count. Test paths follow the same Go, Python and JS/TS conventions as
impact analysis. repocli consumes CodeGraph's `Node.Entrypoint` fact without
reconstructing language rules. Unrecognized entrypoints and public APIs remain
candidates.

## Report

JSON schema 1 contains `snapshot`, `documents`, `nodes` and `diagnostics`.
Nodes retain CodeGraph identity, kind, source location and declaration metadata,
sorted by source path and position. Empty node and diagnostic collections are arrays.
Snapshot completeness describes capture only; graph diagnostics retain parsing and
binding gaps separately. Missing graph edges can reflect these gaps.

The command is read-only. A returned report exits 0, execution failures exit 1,
and invalid CLI arguments exit 2. Candidates do not cause a nonzero exit status.
