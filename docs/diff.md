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

Git supplies the base snapshot. The second snapshot is either the working tree
or the result of applying an input patch to the base in memory. Patch parsing
extracts actual changed line runs, excluding unchanged context. Syntax facts map
those ranges to declarations in each snapshot.

When a change is contained in named declarations, named imports can distinguish
the changed symbols from unrelated symbols in the same module. Importing a
changed symbol suffices; the command does not check whether it is called or read.
Module-level changes and whole-module imports use file granularity. Go uses
package granularity because its imports do not name individual declarations.

Reverse dependency traversal starts from those changed symbols or files. The
graph is the union of both snapshots, so removing an import does not erase its
old dependency before impact is computed. Once a dependent file is reached,
transitive propagation uses file granularity. A sorted breadth-first traversal
provides deterministic shortest explanations and terminates across cycles.

## Why uncertainty is explicit

Syntax alone cannot prove runtime behavior. Symbol-level matching intentionally
does not follow same-file calls, alias assignments, or runtime side effects.
These are limitations of the estimate even when the graph is fully parsed.

Known analysis gaps are a different matter: parse failures, dynamic imports,
unresolved local imports, unsupported resolution configuration, and skipped
files are observable. They broaden selection to every discovered test under the
requested roots and remain visible in `fallbackReasons`. This avoids presenting
a known-incomplete graph as an unexplained empty result. Fatal input or snapshot
failures instead return a nonzero exit without a partial JSON report.

Test discovery follows documented filename conventions, not the project's test
runner configuration. Therefore even a broadened list means all *discovered*
tests, not a claim about a project's complete test inventory.
