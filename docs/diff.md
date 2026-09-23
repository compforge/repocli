# Diff analysis

Repository identity, Snapshot/Comparison, evidence semantics and module boundaries
are defined in the [project kernel](kernel.md). This document describes how `diff`
turns a comparison into changed source and best-effort test recommendations.

## Flow

```text
Git comparison / patch
  → changed files and hunks
  → changed definitions in before / after source
  → changed-file/symbol query entries and test candidates
  → bounded CodeGraph for each version
  → independent reverse queries and merged evidence
  → affected tests and explanations
  → retain evidenced associations and report missing knowledge separately
```

A dependency edge is an import/reference estimate, not proof of a runtime call.
The graph retains intermediate dependent source files in explanation paths;
`sourceFiles` remains the directly changed source list.

Git supplies the base snapshot. The target is the selected working tree, index or
commit, or the result of applying an input patch to the base in memory. Patch parsing
extracts actual changed line runs, excluding unchanged context. Syntax facts map
those ranges to declarations in each snapshot.

When a change is contained in named declarations, named imports can distinguish
the changed symbols from unrelated symbols in the same module. Importing a
changed symbol suffices; the command does not check whether it is called or read.
Module-level changes and whole-module imports use file granularity. Go uses
package granularity because its imports do not name individual declarations.

Following the kernel's version isolation rule, reverse queries use each side's changed
symbols or files. Deleted imports retain their old evidence. Once a dependent file is reached,
transitive propagation uses file granularity. Queries provide deterministic shortest
explanations and terminate across cycles.

`impact` selects a changeset and a candidate testset for each version. A bounded builder
expands their union into a workset of Documents, including intermediate dependencies.
One shared [CodeGraph](codegraph.md) per version receives those documents in a batch;
its declarations supply changed query entries without reparsing each changed file.
Reverse-query results are intersected with the testset. The workset is not a guarantee
of dependency closure: limits and unavailable boundaries remain explicit gaps.

## Best-effort recommendations and execution gaps

Test selection traverses known-target edges, including inferred imports and calls.
Each explanation retains confidence and basis so callers can distinguish static evidence
from inference. Unknown dependency targets are omitted. The analyzer does not propagate
hypothetical missing relations or prove that unselected tests are independent.

Parse failures, unavailable input, unsupported configuration and exhausted workset budgets
remain diagnostics. These actual execution gaps can make `scope: partial` / `complete: false`;
a successful best-effort query does not guarantee dependency coverage. Build gaps are
reported for their snapshot and apply to the requested candidates because an incomplete
graph cannot reliably localize the missing information. Known paths remain useful.
Configuration/resource changes retain their component attribution, and metadata-only
changes remain observations. Fatal input or snapshot failures return a nonzero exit.

Test discovery follows documented filename conventions, not the project's test
runner configuration. The report describes import-based associations, not a
complete runtime test inventory or coverage proof.

README Markdown/reStructuredText changes are documentation observations, not implicit
component-wide source dependencies. Changes limited to descriptive `package.json`
fields are similarly non-blocking. Names, versions, execution/resolution fields and
unknown keys remain configuration changes. Unknown resources are not assumed harmless.
Explicit graph dependencies still participate regardless of metadata classification.

## Repository boundary

Within the kernel's repository boundary, a submodule change remains one gitlink
change. Relative imports into a known gitlink root form edges to that external
entry, so its change can select parent consumers. Captured child JSON resources
may inform config resolution without adding child source or test candidates.

Local configuration and language-specific resolution follow [CodeGraph](codegraph.md).
Resolution failures become dependency diagnostics; they do not independently
select tests. The supported syntax and configuration forms are listed in
[diff usage](diff-usage.md#coverage-and-limits).
