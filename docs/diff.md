# Diff analysis

## Model

A change compares two repository snapshots. Source changes are facts about those
snapshots; test impact is an estimate from syntax and imports. Keeping these
separate allows the command to describe a deletion while returning only existing
affected test files. The command has no policy for consuming either list.

The public boundary is the CLI and its versioned JSON result. The Cobra command layer handles
arguments, streams, and exit codes; analysis coordinates snapshots and ownership
without depending on Cobra. Go packages are implementation details. No plugin runtime or caller-specific state
is required to analyze a repository.

Source identities use quality-harness's neutral Go common package. Component
layout and language discovery are repocli responsibilities; the detected language
is carried by the shared Component as non-identity metadata, following devloop's
manifest boundaries without importing its runtime or command execution rules.
Product membership is a separate registry relationship. Keeping it outside the
shared Component identity allows a common component to serve multiple products.

## Flow

```text
Git comparison / patch
  → changed files and hunks
  → changed definitions in before / after source
  → changed-file roots and test candidates
  → bounded CodeGraph for each version
  → independent reverse queries and merged evidence
  → affected tests and explanations
  → retain evidenced associations and report missing knowledge separately
```

A dependency edge is an import/reference estimate, not proof of a runtime call.
The graph retains intermediate dependent source files in explanation paths;
`sourceFiles` remains the directly changed source list. Review-unit grouping,
execution policy and caller-specific success criteria do not belong in this flow.

Git supplies the base snapshot. The second snapshot is either the working tree
or the result of applying an input patch to the base in memory. Patch parsing
extracts actual changed line runs, excluding unchanged context. Syntax facts map
those ranges to declarations in each snapshot.

When a change is contained in named declarations, named imports can distinguish
the changed symbols from unrelated symbols in the same module. Importing a
changed symbol suffices; the command does not check whether it is called or read.
Module-level changes and whole-module imports use file granularity. Go uses
package granularity because its imports do not name individual declarations.

Reverse queries start from changed symbols or files in separately built before/after
graphs. Their results are merged; edges from incompatible versions are never joined.
Deleted imports retain their old evidence. Once a dependent file is reached,
transitive propagation uses file granularity. Queries provide deterministic shortest
explanations and terminate across cycles.

`impact` supplies roots and candidate tests to the generic [CodeGraph](codegraph.md).
Only these files and their resolved import closure enter source parsing. Graph
construction does not know test-directory or execution policy. File catalog and
manifest indexing remain repository-wide; snapshot byte capture is unchanged.

## Why uncertainty is explicit

Syntax alone cannot prove runtime behavior. Symbol-level matching follows explicit
imports and a bounded subset of same-file calls with unambiguous lexical bindings.
It does not infer dynamic method dispatch, alias assignments, general value references
or runtime side effects. These remain limits even within a fully parsed local scope.

Known analysis gaps are a different matter: parse failures, dynamic imports,
unresolved local imports, unsupported resolution configuration, and skipped
files are observable. They do not create dependency edges or add candidate tests.
The result retains only associations supported by the resolved graph (or a test's
own diff), and reports `scope: partial` / `complete: false` when relevant gaps
remain. An empty partial list does not establish that no tests are affected.
Execution or fallback policy belongs to the caller.

Diagnostics carry file locations. Their relevance follows each version's dependency
graph and component ownership; component roots are not isolation boundaries.
Observed gaps outside the requested candidates' known dependency paths remain visible
in `observations`; unvisited source files produce no syntax diagnostics. Component summaries indicate which parts of the analysis are
incomplete. Snapshot gaps cannot be localized using an incomplete graph. Fatal
input or snapshot failures return a nonzero exit without a partial JSON report.

Test discovery follows documented filename conventions, not the project's test
runner configuration. The report describes import-based associations, not a
complete runtime test inventory or coverage proof.

## Repository boundary

A submodule is a gitlink entry in the parent diff. It is not recursively expanded
into changed child sources, components or test files. Relative imports into a
known gitlink root form edges to that external dependency entry. A changed gitlink
can therefore select parent tests importing it without selecting dependency-owned
tests. Snapshot content identity remains governed separately by the snapshot
contract; capturing dependency content does not make it part of the diff graph.

Local TypeScript config inheritance and explicit workspace package entrypoints
are resolved from captured parent-repository bytes. Conditional exports with
different possible targets, unsupported aliases, generated entrypoints and package
config inheritance remain gaps. Recognized Python literal imports and file-relative
pathlib search roots add dependency facts without evaluating Python. Ambiguous
module matches and truly dynamic expressions produce diagnostics without guessed
edges. All relationships are static estimates, not actual call-site verification.
