"""Git operations with explicit arguments, bounded execution and observable failures."""

from __future__ import annotations

import os
import subprocess
from collections.abc import Callable
from dataclasses import dataclass
from pathlib import Path
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from .git_index import IndexChange


@dataclass(frozen=True)
class GitResult:
    rc: int
    out: str
    err: str
    uncertain: bool = False

    @property
    def ok(self) -> bool:
        return self.rc == 0


def git(
    repo_dir: str | Path,
    *args: str,
    timeout: float = 5,
    raw: bool = False,
    index_file: Path | None = None,
) -> GitResult:
    env = {**os.environ, "GIT_INDEX_FILE": str(index_file.absolute())} if index_file else None
    return _run(["git", "-C", str(repo_dir), *args], timeout, raw=raw, env=env)


def git_global(*args: str, timeout: float = 5) -> GitResult:
    return _run(["git", *args], timeout)


def _run(
    argv: list[str], timeout: float, *, raw: bool = False, env: dict[str, str] | None = None
) -> GitResult:
    try:
        result = subprocess.run(argv, capture_output=True, timeout=timeout, check=False, env=env)
        out = os.fsdecode(result.stdout)
        return GitResult(
            result.returncode,
            out if raw or "\0" in out else out.strip(),
            os.fsdecode(result.stderr).strip(),
        )
    except subprocess.TimeoutExpired as exc:
        # A write may already have taken effect. Callers must inspect before retrying.
        return GitResult(-1, "", str(exc), uncertain=True)
    except OSError as exc:
        return GitResult(-1, "", str(exc))


def stage(
    repo: str | Path,
    paths: list[str],
    *,
    validate: Callable[[list[IndexChange]], None] | None = None,
) -> GitResult:
    """Stage literal paths, retaining unrelated entries.

    With validate, prepare an isolated index and pass its full changes against HEAD
    to the callback. A raised exception propagates and leaves the original index
    untouched, as does a failed add. An empty paths list validates the current index.
    The callback must only inspect facts: the real index is locked until installation.
    Git objects/clean-filter side effects are not rolled back.
    """
    if validate is not None:
        from ._staging import stage_validated

        return stage_validated(repo, paths, validate)
    if not paths:
        raise ValueError("stage requires explicit paths")
    return git(repo, "--literal-pathspecs", "add", "--", *paths, timeout=30)


def create_branch(repo: str | Path, name: str, base: str) -> GitResult:
    """Create and check out a branch at a resolved commit; never stash implicitly."""
    resolved = git(repo, "rev-parse", "--verify", "--end-of-options", f"{base}^{{commit}}")
    return git(repo, "checkout", "-b", name, resolved.out, timeout=30) if resolved.ok else resolved


def commit(repo: str | Path, message: str) -> GitResult:
    """Commit the current index. Selection and validation belong to the caller."""
    return git(repo, "commit", "-m", message, timeout=120)


def push(
    repo: str | Path,
    branch: str,
    *,
    remote: str = "origin",
    upstream: bool = False,
    lease: str | None = None,
) -> GitResult:
    args = ["push"]
    if upstream:
        args.append("--set-upstream")
    if lease is not None:
        args.append(f"--force-with-lease=refs/heads/{branch}:{lease}")
    return git(repo, *args, "--", remote, f"HEAD:refs/heads/{branch}", timeout=120)


def rebase(repo: str | Path, base: str) -> GitResult:
    """Leave conflicts in place for the caller; never auto-abort or discard work."""
    resolved = git(repo, "rev-parse", "--verify", "--end-of-options", f"{base}^{{commit}}")
    return git(repo, "rebase", resolved.out, timeout=120) if resolved.ok else resolved


def continue_rebase(repo: str | Path, *, editor: str | None = None) -> GitResult:
    """Continue staged conflict resolutions; optionally set core.editor for this call.

    Git's editor environment variables still take precedence over core.editor.
    Failure leaves the operation in place for the caller to inspect.
    """
    config = ["-c", f"core.editor={editor}"] if editor is not None else []
    return git(repo, *config, "rebase", "--continue", timeout=120)


def abort_rebase(repo: str | Path) -> GitResult:
    """Ask Git to abort the current rebase and restore its original checkout state."""
    return git(repo, "rebase", "--abort", timeout=30)


def add_worktree(
    repo: str | Path, path: str | Path, ref: str, *, branch: str | None = None, detach: bool = False
) -> GitResult:
    if branch is not None and detach:
        raise ValueError("branch and detach are mutually exclusive")
    args = ["worktree", "add"]
    if detach:
        args.append("--detach")
    if branch is not None:
        args += ["-b", branch]
    return git(repo, *args, "--", str(path), ref, timeout=30)


def remove_worktree(repo: str | Path, path: str | Path, *, force: bool = False) -> GitResult:
    """Default removal preserves dirty and locked worktrees."""
    return git(
        repo,
        "worktree",
        "remove",
        *(["--force", "--force"] if force else []),
        "--",
        str(path),
        timeout=30,
    )


def changed_paths(repo: str | Path, *, base: str = "HEAD", head: str | None = None) -> list[str]:
    """Changed paths, including both rename sides; Git failure remains an error."""
    from ._process import Budget, run

    budget = Budget(10, None)
    root = Path(repo)
    before = (
        run(root, ["rev-parse", "--verify", "--end-of-options", f"{base}^{{commit}}"], budget)
        .decode()
        .strip()
    )
    args = ["diff", "--no-renames", "--name-only", "-z", before]
    if head is not None:
        after = (
            run(root, ["rev-parse", "--verify", "--end-of-options", f"{head}^{{commit}}"], budget)
            .decode()
            .strip()
        )
        args.append(after)
    output = run(root, [*args, "--"], budget)
    if head is None:
        output += run(root, ["ls-files", "--others", "--exclude-standard", "-z"], budget)
    return list(
        dict.fromkeys(os.fsdecode(p) for p in output.split(b"\0") if p and not p.endswith(b"/"))
    )


def committed_paths(repo: str | Path, rev: str = "HEAD") -> list[str]:
    """Paths changed by one commit, against its first parent (empty tree for roots)."""
    from ._process import Budget, run

    budget = Budget(10, None)
    root = Path(repo)
    sha = (
        run(root, ["rev-parse", "--verify", "--end-of-options", f"{rev}^{{commit}}"], budget)
        .decode()
        .strip()
    )
    output = run(
        root,
        [
            "diff-tree",
            "--root",
            "--diff-merges=first-parent",
            "--no-commit-id",
            "-r",
            "--no-renames",
            "--name-only",
            "-z",
            sha,
            "--",
        ],
        budget,
    )
    return list(dict.fromkeys(os.fsdecode(p) for p in output.split(b"\0") if p))


def range_paths(repo: str | Path, base: str, head: str = "HEAD") -> list[str]:
    """Branch changes from the merge base to head, preserving both rename sides."""
    from ._process import Budget, run

    budget = Budget(10, None)
    root = Path(repo)
    refs = [
        run(root, ["rev-parse", "--verify", "--end-of-options", f"{ref}^{{commit}}"], budget)
        .decode()
        .strip()
        for ref in (base, head)
    ]
    output = run(
        root, ["diff", "--no-renames", "--name-only", "-z", f"{refs[0]}...{refs[1]}", "--"], budget
    )
    return list(dict.fromkeys(os.fsdecode(p) for p in output.split(b"\0") if p))
