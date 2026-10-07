"""Git checkout, branch and remote observations."""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import TypedDict

from . import git as gitcmd


def get_current_branch(repo_dir: str | Path) -> str | None:
    r = gitcmd.git(repo_dir, "branch", "--show-current")
    return r.out if r.ok and r.out else None


def get_ahead_behind(repo_dir: str | Path, target: str = "main") -> tuple[int, int] | None:
    """(ahead, behind) relative to origin/<target>. None if target/count unavailable."""
    r = gitcmd.git(repo_dir, "rev-list", "--count", f"origin/{target}..HEAD")
    if not r.ok:
        return None
    try:
        ahead = int(r.out)
    except ValueError:
        return None
    r = gitcmd.git(repo_dir, "rev-list", "--count", f"HEAD..origin/{target}")
    if not r.ok:
        return None
    try:
        behind = int(r.out)
    except ValueError:
        return None
    return ahead, behind


class WorkspaceStatus(TypedDict):
    dirty: bool
    modified_count: int
    untracked_count: int
    complete: bool


def get_workspace_status(repo_dir: str | Path) -> WorkspaceStatus:
    """dict with dirty / modified_count / untracked_count."""
    r = gitcmd.git(repo_dir, "status", "--porcelain")
    if not r.ok:
        return {"dirty": False, "modified_count": 0, "untracked_count": 0, "complete": False}
    lines = [ln for ln in r.out.split("\n") if ln.strip()]
    modified = sum(1 for ln in lines if not ln.startswith("??"))
    untracked = sum(1 for ln in lines if ln.startswith("??"))
    return {
        "dirty": bool(lines),
        "modified_count": modified,
        "untracked_count": untracked,
        "complete": True,
    }


def target_exists(repo_dir: str | Path, target: str = "main") -> bool:
    return gitcmd.git(repo_dir, "rev-parse", "--verify", f"origin/{target}").ok


def refresh_remote_head(repo_dir: str | Path, timeout: int = 5) -> bool:
    """Refresh local origin/HEAD from the remote default branch via one network round-trip
    (`git fetch` never touches origin/HEAD). Best-effort. Used as the no-token fallback when
    the forge can't supply the default branch (see context.repo._resolve_default_branch)."""
    return gitcmd.git(repo_dir, "remote", "set-head", "origin", "--auto", timeout=timeout).ok


def set_local_default_head(repo_dir: str | Path, branch: str) -> bool:
    """Point local `refs/remotes/origin/HEAD` at `branch` — purely local, no network.

    Keeps the git-side cache consistent with an authoritative value resolved elsewhere (the
    forge), so every `local_default_target` caller (gcampr, run_review, …) reads the same answer
    without each re-querying. Best-effort.
    """
    if not branch:
        return False
    return gitcmd.git(
        repo_dir, "symbolic-ref", "refs/remotes/origin/HEAD", f"refs/remotes/origin/{branch}"
    ).ok


def get_worktree_metadata(repo_dir: str | Path) -> tuple[bool, str, str | None]:
    """`(is_linked, common_dir, main_branch)`. One walk because
    `_build_branch_section` needs all three together."""
    r1 = gitcmd.git(repo_dir, "rev-parse", "--git-dir")
    r2 = gitcmd.git(repo_dir, "rev-parse", "--git-common-dir")
    if not r1.ok or not r2.ok or not r1.out or not r2.out:
        return False, "", None
    base = Path(repo_dir)
    try:
        gd = (base / r1.out).resolve()
        cd = (base / r2.out).resolve()
    except OSError:
        return False, "", None
    if gd == cd:
        return False, "", None  # main checkout
    common_dir = str(cd)
    main_branch: str | None = None
    r = gitcmd.git(repo_dir, "worktree", "list", "--porcelain")
    if r.ok and r.out:
        first_block = r.out.split("\n\n", 1)[0]
        for line in first_block.splitlines():
            if line.startswith("branch "):
                ref = line[len("branch ") :].strip()
                if ref.startswith("refs/heads/"):
                    main_branch = ref[len("refs/heads/") :]
                break
    return True, common_dir, main_branch


def is_linked_worktree(repo_dir: str | Path) -> bool:
    """True iff a linked worktree (git-dir != git-common-dir). False on error
    → callers treat as main checkout (safe default)."""
    r1 = gitcmd.git(repo_dir, "rev-parse", "--git-dir")
    r2 = gitcmd.git(repo_dir, "rev-parse", "--git-common-dir")
    if not r1.ok or not r2.ok or not r1.out or not r2.out:
        return False
    try:
        return (Path(repo_dir) / r1.out).resolve() != (Path(repo_dir) / r2.out).resolve()
    except OSError:
        return False


def get_head_sha(repo_dir: str | Path) -> str:
    """Current HEAD sha (full). Empty on error. Works on detached HEAD (unlike a
    branch-name read), which is exactly why gates resolve identity through git, not a lib."""
    r = gitcmd.git(repo_dir, "rev-parse", "HEAD")
    return r.out if r.ok else ""


def rev_parse(repo_dir: str | Path, ref: str) -> str:
    """Resolve a ref to a sha (verified). Empty when the ref is absent/unresolvable —
    e.g. a not-yet-fetched `origin/<branch>`."""
    r = gitcmd.git(repo_dir, "rev-parse", "--verify", "--quiet", ref)
    return r.out if r.ok else ""


def is_ancestor(repo_dir: str | Path, ancestor: str | None, descendant: str | None) -> bool:
    """True iff `ancestor` is reachable from `descendant` (`merge-base --is-ancestor`).
    Empty/error → False: callers treat 'unknown' as 'not reachable', which is the safe
    default for PR selection (a dead-ref PR whose sha we can't reach is not the branch's PR)."""
    if not ancestor or not descendant:
        return False
    if ancestor == descendant:
        return True
    return gitcmd.git(repo_dir, "merge-base", "--is-ancestor", ancestor, descendant).rc == 0


def get_upstream_ahead_behind(repo_dir: str | Path) -> tuple[int, int] | None:
    """(ahead, behind) of HEAD vs its OWN upstream (`origin/<current>`), or None when the
    branch has no upstream (fresh feature branch) or it can't be resolved. This is the
    "my branch moved on the server (pushed from elsewhere)" signal — distinct from
    behind-trunk. Bounded to local refs, no network (the upstream ref is whatever the last
    fetch left)."""
    r = gitcmd.git(repo_dir, "rev-list", "--count", "--left-right", "@{upstream}...HEAD")
    if not r.ok or "\t" not in r.out:
        return None
    behind_s, ahead_s = r.out.split("\t", 1)
    try:
        return int(ahead_s), int(behind_s)
    except ValueError:
        return None


def ls_remote_tips(repo_dir: str | Path, *branches: str, timeout: int = 5) -> dict[str, str]:
    """`{branch: sha}` for `branches` on origin via `git ls-remote` — the TRUE remote tip,
    one network round-trip, NO object fetch. The monitor's cheap way to learn that trunk
    moved (a colleague pushed) without pulling history. Empty dict offline/on error."""
    if not branches:
        return {}
    r = gitcmd.git(repo_dir, "ls-remote", "origin", *branches, timeout=timeout)
    if not r.ok:
        return {}
    tips: dict[str, str] = {}
    for line in r.out.splitlines():
        if "\t" not in line:
            continue
        sha, ref = line.split("\t", 1)
        if ref.startswith("refs/heads/"):
            tips[ref[len("refs/heads/") :]] = sha
    return tips


def list_worktrees(repo_dir: str | Path) -> list[tuple[str, str, str | None]]:
    """Registered worktrees as (path, SHA, branch); paths are NUL delimited.

    The first entry may be a bare/separate Git directory, not a checkout. Use
    checkout_info for topology. Query failures raise rather than imply an empty repo.
    """
    r = gitcmd.git(repo_dir, "worktree", "list", "--porcelain", "-z")
    if not r.ok:
        raise OSError(r.err or "cannot list worktrees")
    out = []
    for block in r.out.split("\0\0"):
        fields = dict(line.split(" ", 1) for line in block.split("\0") if " " in line)
        if path := fields.get("worktree"):
            branch = fields.get("branch")
            out.append(
                (
                    path,
                    fields.get("HEAD", ""),
                    branch.removeprefix("refs/heads/") if branch else None,
                )
            )
    return out


@dataclass(frozen=True)
class CheckoutInfo:
    root: str
    git_dir: str
    common_dir: str
    main_root: str | None

    @property
    def linked(self) -> bool:
        return self.git_dir != self.common_dir


def checkout_info(repo_dir: str | Path) -> CheckoutInfo:
    """Observe checkout topology, without choosing a caller's state directory.

    main_root is the checkout recoverable from shared Git metadata. It can be
    unknown for separate Git directories: their original checkout has no backlink.
    Non-checkouts and failed observations raise OSError.
    """

    def query(path: str | Path, flag: str) -> str:
        result = gitcmd.git(path, "rev-parse", "--path-format=absolute", flag, raw=True)
        if not result.ok or not result.out:
            raise OSError(result.err or f"cannot resolve {flag}")
        return str(Path(result.out.removesuffix("\n")).resolve())

    root = query(repo_dir, "--show-toplevel")
    gd = query(repo_dir, "--git-dir")
    cd = query(repo_dir, "--git-common-dir")
    entries = list_worktrees(repo_dir)
    main = None
    if entries:
        try:
            candidate = query(entries[0][0], "--show-toplevel")
            # Git can treat a separate metadata directory as the work tree. Its
            # lack of a .git entry distinguishes it from an actual checkout.
            if (Path(candidate) / ".git").exists() and query(candidate, "--git-common-dir") == cd:
                main = candidate
        except OSError:
            pass
    return CheckoutInfo(root, gd, cd, main)


def main_repo_root(repo_dir: str) -> str | None:
    """Verified main checkout, or None when Git cannot identify it."""
    try:
        return checkout_info(repo_dir).main_root
    except OSError:
        return None


def list_local_branches(repo_dir: str | Path) -> list[tuple[str, str]]:
    """Every local branch as ``(name, head_sha)`` in stable name order.

    A PR/MR can outlive its checkout, so lifecycle inventory starts from local
    refs rather than from ``git worktree list``. The NUL separator avoids
    parsing human-facing branch output.
    """
    r = gitcmd.git(
        repo_dir,
        "for-each-ref",
        "--sort=refname",
        "--format=%(refname:short)%00%(objectname)",
        "refs/heads",
    )
    if not r.ok or not r.out:
        return []
    out: list[tuple[str, str]] = []
    for line in r.out.splitlines():
        if "\x00" not in line:
            continue
        branch, sha = line.split("\x00", 1)
        if branch and sha:
            out.append((branch, sha))
    return out


def fetch(repo_dir: str | Path, *refs: str, timeout: int = 8) -> bool:
    """Bounded `git fetch origin [refs...]` for low-freq, intentional boundaries.
    Refreshes local remote-tracking refs so behind/ahead become REAL rather than
    relative-to-a-stale-mirror. Best-effort (offline → False)."""
    return gitcmd.git(repo_dir, "fetch", "origin", *refs, "--quiet", timeout=timeout).ok
