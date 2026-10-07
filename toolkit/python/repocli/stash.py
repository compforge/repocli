"""Explicit stash identities and index-preserving restoration."""

from dataclasses import dataclass
from pathlib import Path
from uuid import uuid4

from . import git


@dataclass(frozen=True)
class SavedStash:
    result: git.GitResult
    oid: str | None = None


def save(repo: str | Path, *, message: str = "", include_untracked: bool = False) -> SavedStash:
    """Save changes and return this invocation's object ID, or None for a no-op.

    The stash remains in Git's stash list. An unsuccessful/uncertain result must
    be inspected before continuing; an available oid identifies the recovery copy.
    """
    marker = f"repocli-stash-{uuid4().hex}"
    result = git.git(
        repo,
        "stash",
        "push",
        *(["--include-untracked"] if include_untracked else []),
        "--message",
        f"{marker} {message}",
        timeout=30,
    )
    # why: refs/stash is shared by worktrees. Its tip may already belong to another
    # writer; identify our unique message instead of interpreting localized output.
    listing = git.git(
        repo,
        "stash",
        "list",
        "--format=%H",
        "--fixed-strings",
        f"--grep={marker}",
        "--max-count=1",
    )
    if not listing.ok:
        return SavedStash(
            git.GitResult(
                -1,
                result.out,
                f"cannot identify saved stash {marker}: {listing.err}",
                uncertain=True,
            )
        )
    return SavedStash(result, listing.out or None)


def restore(repo: str | Path, oid: str) -> git.GitResult:
    """Apply one stash object, restoring staged/unstaged separation.

    The recovery copy is retained, including after success. No shared stash entry
    is dropped by position; conflicts and uncertain outcomes remain for inspection.
    """
    resolved = git.git(repo, "rev-parse", "--verify", "--end-of-options", f"{oid}^{{commit}}")
    return (
        git.git(repo, "stash", "apply", "--index", resolved.out, timeout=30)
        if resolved.ok
        else resolved
    )
