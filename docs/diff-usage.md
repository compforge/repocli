# repocli diff

[Project overview](../README.md) · [中文项目介绍](../README.zh-CN.md)

`repocli diff` describes what changed and which files may be affected. It
reports source files, changed declarations, and import dependency paths. It does
not execute project commands or decide what a caller should do with the result.

## What it reports

- `affectedFiles`: reached existing files in the analyzed workset, plus changed existing
  files. Each entry includes `path`, snapshot `version`, `seed`, path `confidence`,
  dependency `distance`, `dependencyPath` and raw `relations`.
- `seeds`: automatic before/after query entries, with `id`, `path`, `version`,
  `granularity` (symbol, module, file or package) and the selection `basis`.

- `sourceFiles`: changed Go, Python, JavaScript, and TypeScript source paths.
  This includes changed test sources and deleted sources. A rename uses its new
  path; the old path and status remain in `changes`.
- `testFiles`: existing test files under the requested directories that may be
  affected through a known-target dependency path, including inferred edges, or
  are themselves changed. Unknown-target relationships and deleted tests are omitted.
- `changes`: all changed files, their status, changed line ranges, and declarations
  intersecting those ranges in the before/after snapshots. Symbols include
  `qualifiedName` to distinguish owners such as `A.work` and `B.work`.
- `reasons`: dependency paths explaining the affected test files, with typed
  `relations` and source locations where available. `version` identifies the
  before/after snapshot supplying the evidence.
- `fallbackReasons`: retained wire name for reasons the analysis is incomplete;
  repocli does not fill the test list or choose an execution fallback.
- `observations`: non-blocking gaps and metadata-only changes. These retain reporting context without creating dependency edges.
- `repository` and `components`: repository identity, component roots and
  languages, declared product memberships, and per-component source/test lists.

For example, if `source.ts` exports `a` and `b`, changing the body of `a` selects
a test importing `{ a as alias }`, even if it never calls `alias`. A test importing
only `{ b }` is not selected by that direct dependency. Namespace, default and
side-effect imports are treated at module granularity. Go imports are treated at
package granularity. Transitive propagation is deliberately broader: once an
importing file is affected, its downstream importers may also be affected.

This is a **static import-based estimate**, not proof that other tests are
unaffected. It follows the static calls supplied by CodeGraph, preserving cross-file
targets and candidate evidence, so a helper change can reach tests importing its caller. It does not
check actual use of imports in tests or infer dynamic method dispatch, general value
references, runtime side effects, reflection, arbitrary resource access, or framework-specific
implicit setup dependencies beyond recognized configuration files (such as
`conftest.py`).

## Quick start

Requires Go 1.26+ to build and Git to read repositories. No Node or Python runtime
is needed for analysis.

```sh
make build
./bin/repocli diff --repo /path/to/repo --base main --test-dir tests --json
```

Repeat `--test-dir` for multiple directories. Use `--test-dir .` for colocated
tests throughout a repository. Test directories only limit candidate discovery;
sources and imported helpers inside them still use ordinary dependency analysis.
Paths in the report and test-directory arguments
are relative to the repository root. Without `--test-dir`, the command reports
affected files using all supported source files as candidate roots. With test directories,
`affectedFiles` covers the changeset/testset dependency workset. Both forms are bounded
by 2,000 parsed files and depth 32 per snapshot; no result promises a complete closure.

`--base` defaults to `HEAD` and must resolve to a commit. The comparison is that
exact commit versus the current working tree, including staged, unstaged, and
non-ignored untracked files. It does **not** implicitly find a merge base.

You can also supply a Git patch, including via stdin:

```sh
git diff --binary main > /tmp/change.patch
./bin/repocli diff --base main --file /tmp/change.patch --test-dir tests --json

git diff --binary HEAD | ./bin/repocli diff --file - --test-dir tests --json
```

Patch mode reconstructs the postimage from `--base` in memory, independently of
the working tree. The base must match the patch preimage. Use `--binary` when a
patch includes binary changes. This mode does not include untracked files unless
the patch itself contains them.

## Output

The default output is readable text; `--json` writes one JSON object to stdout.
Text groups local gaps by reason and subject, showing counts and up to three examples;
JSON retains every recorded location.
Execution errors go to stderr; analysis diagnostics are included in the result. An abbreviated result looks like:

```json
{
  "schemaVersion": 3,
  "complete": true,
  "diagnostics": [],
  "sourceFiles": ["src/source.ts"],
  "testFiles": ["tests/source.test.ts"],
  "scope": "focused",
  "reasons": [
    {
      "testFile": "tests/source.test.ts",
      "kind": "import",
      "dependencyPath": ["tests/source.test.ts", "symbol:src/source.ts#a"]
    }
  ],
  "fallbackReasons": []
}
```

`scope` describes the requested impact analysis:

| Value | Meaning |
| --- | --- |
| `focused` | Known static paths determined the returned files. |
| `partial` | Known paths are returned; relevant input, workset or configuration gaps remain. |

An empty `testFiles` is only meaningful together with `scope` and
`complete` and `diagnostics`. A partial empty list does not mean no tests were
affected. A successful exit means analysis ran, possibly with gaps; it says
nothing about project correctness. The caller owns any execution fallback. Exit code `1`
indicates analysis/input failure and `2` indicates command-line usage errors.

## Coverage and limits

Test discovery recognizes `_test.go`, `test_*.py`, `*_test.py`, JS/TS
`*.test.*` / `*.spec.*`, and JS/TS sources inside `__tests__`. Custom test filename
conventions are not discovered. Dependencies outside test directories are still
read so indirect imports can be followed.

- JS/TS: relative imports, named imports and aliases, re-exports, plain-string
  `require` and `import()`, and declared external package dependencies. Local JSON
  `tsconfig extends` (including arrays) and explicit workspace `exports`, `main`,
  and `module` source entrypoints are resolved. Referenced submodule JSON configs
  are read from the selected snapshot without discovering child sources or tests. Captured conditional export alternatives become weak edges. Custom TS aliases,
  package-based `extends`, JSONC config syntax,
  export patterns/arrays and missing generated entrypoints have no resolved target.
  Configuration parsing failures remain diagnostics.
- Python: relative imports, named imports and package initialization. Absolute
  imports resolve against proven search-path prefixes; catalog-only source-root
  matches remain weak inference, even when unique. File-local variables, import
  aliases, small literal loops, and roots built from `Path(__file__)`, `resolve()`,
  `parent` / `parents[n]`, and `/` literal suffixes are recognized. Context follows
  statement order and respects binding shadowing. `sys.path.insert` can establish
  a known prefix; append, conditional roots and deferred function bodies do not
  establish precedence over unknown paths. Known-string `importlib.import_module()`
  / `__import__()` targets are supported. Cwd-relative strings, external roots,
  unsupported path mutations invalidate known precedence. Unknown dynamic targets are
  omitted; unavailable captured roots and evaluation budget exhaustion remain diagnostics.
  Absolute imports with no repository match are treated as external. Import hooks,
  module caches, arbitrary function execution and cross-module side effects are
  outside this static model.
- Go: repository module imports and implicit same-package dependencies, across
  all source files regardless of build tags. Local `replace` directives cause
  diagnostics. Results remain file paths, including for Go tests.

Configuration changes and unsupported resources retain scoped diagnostics. README
Markdown/reStructuredText changes and changes limited to descriptive package fields
are observations; package versions, names, exports, scripts and unknown fields are
not exempt. Local parse, declaration and call-resolution gaps remain observations,
including their snapshot, subject, location and outline counters where available.
Overlapping declaration gaps broaden changed seeds to files; they do not make all
candidates partial. Unresolved targets stay unknown.
These gaps never add tests. Both old and new import graphs participate, preserving resolved
dependencies removed by the diff.

Untracked embedded repositories (including linked worktrees) are outside the parent
repository input; ordinary untracked files remain included. Git-ignored untracked
files are excluded. Symlink, submodule and large-file content
identity follows [snapshot](snapshot.md). A submodule remains one gitlink entry in
`changes`; its sources, components and tests are not recursively included. Parent
imports into a gitlink root depend on that entry as an external package. A gitlink
change can select parent consumers, but never dependency-owned tests. Unchanged
non-source links/assets do not independently create analysis gaps; imports of
unavailable targets, source symlinks and changed resources remain diagnosed.
Files over 2 MiB are hashed without syntax parsing. Capture limits are 10,000 files
or 128 MiB of regular-file contents per repository; exceeding them fails rather
than silently truncating. The default deadline is
two minutes, configurable with `--timeout`. Syntax facts for identical content
are reused within a run; there is no persistent cache.

See [the analysis design](diff.md) for implementation boundaries.

## Repository structure

The [project kernel](kernel.md) defines Repository, Component, Product and Forge
identities and their relationship to a local checkout. The following rules describe
how `diff` discovers that context and exposes it in its report.

Component discovery follows devloop's project-boundary rules: `pyproject.toml`,
`setup.py`, `go.mod`, and `package.json` identify components. Makefiles,
`requirements.txt`, and `tsconfig.json` alone do not create extra component
boundaries. A recognized non-root component stops nested discovery; a recognized
repository-root component can coexist with child components. Dependencies,
generated directories, hidden directories, and `testdata` are excluded from
discovery. With no project manifests, one root component is reported.

Each component entry has a repository-relative `root`; its detected language
is reported in the shared `component.language` metadata. The common SDK derives
the tool ecosystem from that language: Python → `python`, Go → `go`, and
JavaScript/TypeScript → `node`. Ecosystem is not a separate configuration or
JSON field. This mapping does not identify a package manager such as uv or pnpm.
Each component also reports `scope`, `complete`, and optional `fallbackReasons` for
the requested analysis. Cross-component consumers participate in the same graph;
component roots are never treated as proof of dependency isolation.
Discovery uses the Python → Go → Node precedence when a directory has multiple
manifests. TypeScript package metadata or `tsconfig.json` distinguishes TS from
JS. Source-only layouts use file extensions, reporting `mixed` for multiple known
languages and omitting language when it is unknown. No project code is executed.

The report includes the entire current component catalog, even components with
empty change lists. Files belong to the most specific component root; shared
root files in a `server/` + `cli/` repository can remain unowned. Removed
components that own changed source files retain `snapshot: "before"` metadata.

An optional, versioned `.repocli.json` overrides identities and layout:

```json
{
  "repository": {"forge": {"name": "github"}, "path": "example/mono"},
  "components": [
    {"name": "api", "root": "server", "products": [{"name": "example-product"}]},
    {"name": "client", "root": "cli", "products": [{"name": "example-product"}]}
  ]
}
```

`language` can be supplied per component or detected; it does not participate
in component identity. Unknown language is omitted. Unknown, unsupported, or
`mixed` languages have no derived ecosystem. Without an explicit
repository identity, `origin` supplies it. If neither is available, the top-level
repository is `null` and component repository fields have zero values; a local
checkout path is never substituted for forge identity. Product memberships
default to an empty list. The top-level `checkout` reports local location
separately from stable identity.

## Comparison and automation contract (schema 3)

`--base REF` is an exact commit, never an implicit merge base. The target is the
working tree by default; `--staged` selects the index and `--head REF` selects a
commit. These target modes and `--file` are mutually exclusive. Working-tree
analysis includes non-ignored untracked files. No mode writes to the index.

Repeat `--changed-file PATH` to restrict changed seeds (matching either rename
side). The complete before/after snapshots and dependency graph remain available,
so unchanged consumers in another component can still appear in `testFiles`.

Granularity is automatic; there is no `--impact` option. Edits within known declarations
use symbol seeds plus a whole-module-import seed. Module-level edits, changed tests,
added/renamed files, and changes overlapping declaration gaps use file seeds. Go uses
package propagation. `seed.basis` explains the choice; seed precision is separate from
path confidence.

Path confidence follows the weakest edge: exact, strong, then weak. Native CodeGraph
candidate edges rank as weak while retaining their original value in `relations`.
Stronger paths win before shorter paths. Distance counts imports, calls and config
inheritance; ownership, package membership and export aliases count as zero. Distance
and confidence are ranking signals, not probabilities. Before/after edges are never
combined into one path. Changed existing files have an exact, zero-distance direct
change explanation; deleted files remain seeds, but are absent from `affectedFiles`.

JSON includes `schemaVersion: 3`, resolved `base`/`head`, `input`,
`snapshot` (SHA-256 of observed file contents), `complete`, and `diagnostics`.
Diagnostic codes are `snapshot_incomplete`, `snapshot_changed`, and
`impact_uncertain`; `message` describes the gap and `path` is optional. Impact
diagnostics additionally expose `reason` (such as `missing_config`,
`boundary_unavailable`, `expansion_limit` or `configuration_change`), `relation`,
`version` and `line` where available. Relation `confidence` is omitted for definite
static evidence, `strong` for strong inference, or `weak` for weak inference; `basis`
explains the inference. Native CodeGraph calls retain `exact` / `candidate`, relation ID and full location.
All known-target tiers participate in recommendations; unknown targets create no edge.

`complete` means no blocking input, workset or repository-resolution gaps in the requested
best-effort analysis. Local extraction gaps remain in `observations` without changing it.
It does not guarantee dependency coverage: even a focused, complete report with an
empty test list cannot prove that no tests are affected. Snapshot gaps are reported
even without test directories or changed files. Callers decide whether to run more tests.

Working tree/index contents are compared before and after analysis to detect
concurrent changes. The digest is content identity, **not an atomic snapshot**.
Callers must ensure the checked tree still matches the analyzed input.

Use `repocli --version` for build identity. Tagged releases publish fixed-version
macOS/Linux amd64/arm64 archives and SHA-256 checksums. Consumers should install
once outside validation, keep a pinned version, and check the JSON schema before
using a result. A missing CLI or unsupported schema is an analysis failure, never
evidence that no files were affected.

For complete regular-file snapshots, `snapshot` hashes paths in lexicographic order.
Each entry contributes UTF-8 byte length, `:`, path bytes, decimal content byte length,
`:`, then content bytes. Prefix the SHA-256 hex digest with `sha256:`. This framing
lets a consumer compare execution input without rerunning dependency analysis.
Incomplete snapshots additionally hash their sorted issue strings and must never
be treated as a complete regular-file identity.

Execution logging is shared by all commands; see [execution logs](logging.md).

Snapshot completeness and dependency-analysis completeness are separate: an internal
symlink or initialized dirty submodule can have a reliable content identity while
parent-repository expansion failures can still yield `impact_uncertain`, and parse failures
remain local observations. Patch reconstruction with symlinks,
submodules or streamed large files in the base is unsupported; use working-tree,
index or commit comparison instead. Imports without a captured target, including runtime built-ins and unknown package
specifiers, create neither edges nor unresolved-package diagnostics.
