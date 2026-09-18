# repocli

Repository change analysis for developers and coding agents.

`repocli diff` describes what changed and which test files may be affected. It
reports source files, changed declarations, and import dependency paths. It does
not execute project commands or decide what a caller should do with the result.

## What it reports

- `sourceFiles`: changed Go, Python, JavaScript, and TypeScript source paths.
  This includes changed test sources and deleted sources. A rename uses its new
  path; the old path and status remain in `changes`.
- `testFiles`: existing test files under the requested directories that may be
  affected. Deleted tests are not included.
- `changes`: all changed files, their status, changed line ranges, and declarations
  intersecting those ranges in the before/after snapshots.
- `reasons`: import/dependency paths explaining the affected test files.
- `fallbackReasons`: uncertainty that caused the analysis to include every
  discovered test under the requested directories.
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
Diagnostics go to stderr. An abbreviated result looks like:

```json
{
  "schemaVersion": 1,
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
| `fallback` | Uncertainty broadened the list to all discovered tests in the requested directories. |

An empty `testFiles` is only meaningful together with `scope` and
`fallbackReasons`. A successful exit means analysis completed, including an
explicit fallback; it says nothing about project correctness. Exit code `1`
indicates analysis/input failure and `2` indicates command-line usage errors.

## Coverage and limits

Test discovery recognizes `_test.go`, `test_*.py`, `*_test.py`, JS/TS
`*.test.*` / `*.spec.*`, and JS/TS sources inside `__tests__`. Custom test filename
conventions are not discovered. Dependencies outside test directories are still
read so indirect imports can be followed.

- JS/TS: relative imports, named imports and aliases, re-exports, plain-string
  `require` and `import()`, and declared external package dependencies. Custom TS
  path resolution and workspace package exports cause explicit fallback.
- Python: absolute and relative imports, named imports, package initialization,
  and possible source-root matches. When multiple repository modules match, all
  are retained. Absolute imports with no repository match are treated as external.
- Go: repository module imports and implicit same-package dependencies, across
  all source files regardless of build tags. Local `replace` directives cause
  fallback. Results remain file paths, including for Go tests.

Configuration changes, unsupported resource changes, detected dynamic imports,
parse failures, or unresolved local dependencies broaden the result. Both old and
new import graphs participate, preserving dependencies removed by the diff.

Git-ignored untracked files are excluded. Symlinks and submodules are not followed
and are reported as analysis gaps. Individual files over 2 MiB are skipped with
an explicit gap; repositories over 10,000 regular files or 128 MiB per snapshot
fail rather than produce a silently truncated report. The default deadline is
two minutes, configurable with `--timeout`. Syntax facts for identical content
are reused within a run; there is no persistent cache.

See [the analysis design](docs/diff.md) for implementation boundaries.

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
