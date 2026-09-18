# repocli diff

[Project overview](../README.md) · [中文项目介绍](../README.zh-CN.md)

`repocli diff` describes what changed and which test files may be affected. It
reports source files, changed declarations, and import dependency paths. It does
not execute project commands or decide what a caller should do with the result.

## What it reports

- `sourceFiles`: changed Go, Python, JavaScript, and TypeScript source paths.
  This includes changed test sources and deleted sources. A rename uses its new
  path; the old path and status remain in `changes`.
- `testFiles`: existing test files under the requested directories that may be
  affected through a resolved dependency path, or are themselves changed.
  Uncertain associations and deleted tests are not included.
- `changes`: all changed files, their status, changed line ranges, and declarations
  intersecting those ranges in the before/after snapshots.
- `reasons`: import/dependency paths explaining the affected test files.
- `fallbackReasons`: retained wire name for reasons the analysis is incomplete;
  repocli does not fill the test list or choose an execution fallback.
- `observations`: gaps outside the requested candidates' known dependency paths;
  these stay visible without adding speculative associations.
- `repository` and `components`: repository identity, component roots and
  languages, declared product memberships, and per-component source/test lists.

For example, if `source.ts` exports `a` and `b`, changing the body of `a` selects
a test importing `{ a as alias }`, even if it never calls `alias`. A test importing
only `{ b }` is not selected by that direct dependency. Namespace, default and
side-effect imports are treated at module granularity. Go imports are treated at
package granularity. Transitive propagation is deliberately broader: once an
importing file is affected, its downstream importers may also be affected.

This is a **static import-based estimate**, not proof that other tests are
unaffected. It does not analyze actual symbol use, same-file call relationships,
runtime side effects, reflection, arbitrary resource access, or framework-specific
implicit setup dependencies beyond recognized configuration files (such as
`conftest.py`).

## Quick start

Requires Go 1.25+ to build and Git to read repositories. No Node or Python runtime
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
changes and source files, with test analysis marked `not_requested`.

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
Execution errors go to stderr; analysis diagnostics are included in the result. An abbreviated result looks like:

```json
{
  "schemaVersion": 2,
  "complete": true,
  "diagnostics": [],
  "impactMode": "symbol",
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

`scope` describes test selection:

| Value | Meaning |
| --- | --- |
| `not_requested` | No test directories were supplied. |
| `focused` | Static dependencies determined the returned test list. |
| `partial` | Only evidenced test associations are returned; relevant analysis gaps remain. |

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
  and `module` source entrypoints are resolved. Conditional exports must identify
  one target; conflicting entrypoints remain gaps. Custom TS aliases,
  package-based `extends`, JSONC config syntax,
  export patterns/arrays and missing generated entrypoints remain diagnostics.
- Python: absolute and relative imports, named imports, package initialization,
  and source-root matches. Multiple matching modules are diagnosed as ambiguous
  rather than guessed. Literal `importlib.import_module()` / `__import__()` targets and
  inline `sys.path.insert/append` roots built from `Path(__file__)`, `resolve()`,
  `parent` / `parents[n]`, and `/` literal suffixes are recognized. Cwd-relative
  strings, variables, external paths and runtime expressions remain gaps.
  Absolute imports with no repository match are treated as external.
- Go: repository module imports and implicit same-package dependencies, across
  all source files regardless of build tags. Local `replace` directives cause
  diagnostics. Results remain file paths, including for Go tests.

Configuration changes, unsupported resource changes, detected dynamic imports,
parse failures, or unresolved local dependencies make analysis partial; they do
not add tests. Both old and new import graphs participate, preserving resolved
dependencies removed by the diff.

Git-ignored untracked files are excluded. Symlink, submodule and large-file content
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

Repository, Component, Product and Forge identities come directly from
[`quality-harness` Go common](https://github.com/compforge/quality-harness/tree/main/sdks/go/common).
Repository identity is its forge plus repository path; a component belongs to
one repository. A product can use multiple components, and a component can serve
multiple products. Products are declared, never guessed from a repository name.

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

## Comparison and automation contract (schema 2)

`--base REF` is an exact commit, never an implicit merge base. The target is the
working tree by default; `--staged` selects the index and `--head REF` selects a
commit. These target modes and `--file` are mutually exclusive. Working-tree
analysis includes non-ignored untracked files. No mode writes to the index.

Repeat `--changed-file PATH` to restrict changed seeds (matching either rename
side). The complete before/after snapshots and dependency graph remain available,
so unchanged consumers in another component can still appear in `testFiles`.

`--impact symbol` is the default named-import heuristic. `--impact file` propagates
from the whole changed file, including consumers of unchanged declarations in that
file. It is the conservative choice for automated validation; neither mode proves
runtime coverage or executes tests.

JSON includes `schemaVersion: 2`, resolved `base`/`head`, `input`, `impactMode`,
`snapshot` (SHA-256 of observed file contents), `complete`, and `diagnostics`.
Diagnostic codes are `snapshot_incomplete`, `snapshot_changed`, and
`impact_uncertain`; `message` describes the gap and `path` is optional. Snapshot
gaps are reported even without test directories or changed files. `complete`
means no blocking gaps in the requested analysis, not complete runtime dependency
coverage. Non-blocking `observations` remain inspectable. Consumers that require
complete analysis must reject `complete: false`, including `scope: partial`.
They may choose broader validation themselves; repocli only reports known
associations and analysis gaps. Consumers requiring the existing schema-2
`complete: true` / `scope: focused` pair continue to fail closed.
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
unresolved parent-repository dependencies still yield `impact_uncertain`. Patch reconstruction with symlinks,
submodules or streamed large files in the base is unsupported; use working-tree,
index or commit comparison instead. Recognized Bun built-ins (`bun`, `bun:test`,
`bun:sqlite`, `bun:ffi`, `bun:jsc`) do not create unresolved-package diagnostics;
unknown `bun:` specifiers remain diagnostics.
