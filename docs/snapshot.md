# Snapshot

`snapshot` identifies repository contents so callers can compare the input to an
analysis with the contents present before or after another operation. It does not
create a commit, backup or atomic filesystem snapshot, and does not perform syntax,
component or test-impact analysis.

```sh
repocli snapshot --repo /path/to/repo --json
repocli snapshot --staged --json
repocli snapshot --head HEAD --json
```

The default reads current bytes of tracked and non-ignored untracked files, including
staged and unstaged changes and excluding deleted files. It works before the first
commit and from linked worktrees or repository subdirectories. `--staged` reads
index blobs; `--head` resolves a commit once and reads its tree. These options are
mutually exclusive. The command inherits `--timeout`, text/JSON output and execution
logging from the CLI.

Untracked embedded Git repositories, including linked worktrees inside the checkout,
are separate repository boundaries and do not enter the parent snapshot or diff.
Run the command inside that checkout to inspect it. Ordinary untracked files in
sibling directories remain included unless Git ignore rules exclude them. Tracked submodules follow the gitlink rules below.

## Report and comparison

The command's JSON schema is version 1, independent of the diff report's schema:

- `checkout`: resolved working-tree root; `input`: `working_tree`, `index` or `commit`.
- `head`: resolved commit ID, only for commit input.
- `snapshot`: `sha256:` digest; `fileCount`: included regular files.
- `complete`: whether capture completed without known gaps or detected changes.
- `diagnostics`: codes, messages and file paths when available; an empty array on a complete capture.

The digest is the same sorted, length-framed path/content hash used by `diff`.
Identical complete contents produce the same digest across working tree, index and
commit inputs. It includes all captured regular files, not just source files or
changed files. File modes, timestamps, ignored files and external dependencies are
outside this content identity. Incomplete captures also hash their issue messages.

Internal symlinks contribute their link text and refer to already captured files;
links are never followed outside the captured input. External, cyclic, missing or
ignored targets remain incomplete. Initialized submodules contribute their resolved
commit and a recursive content digest; working-tree capture includes staged,
unstaged and non-ignored untracked child files. Index/commit capture reads the exact
gitlink commit from the available child repository. No submodule is initialized or
fetched automatically; unavailable children remain incomplete. Recursion is bounded
to eight submodule levels. Analysis may retain captured child JSON bytes in a
separate resource catalog for explicit config inheritance. These bytes are already
covered by the child digest; they do not change the identity format, parent
`fileCount`, or parent source/component/test discovery.

Files larger than 2 MiB are streamed into a size/content hash rather than loaded for
syntax analysis. Typed symlink, submodule and large-file records follow regular
file records in the digest, sorted by path within each type. Regular-only digest
compatibility is preserved. `fileCount` counts regular files in the selected root,
including streamed large files; submodule contents contribute through their digest.

Mutable inputs are read twice; differing observations produce `snapshot_changed`.
The shared reader limits each repository to 10,000 candidate files and 128 MiB of
in-memory regular-file content. Streaming honors the command deadline. Known gaps
produce `snapshot_incomplete`; read/Git failures exit 1 without a report. A returned
report, including an incomplete one, exits 0. Invalid CLI usage exits 2.

Compare only compatible digest contracts with `complete: true`; check the intended
checkout and input source separately. A diff report may be incomplete because of
impact uncertainty even when a standalone content snapshot is complete. Equal
digests are observed content equality, not proof of an atomic read or test coverage.
Callers own any decision to reuse a result, including command/tool/environment inputs.
