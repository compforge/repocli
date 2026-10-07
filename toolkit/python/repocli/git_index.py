"""Working-tree and index facts, preserving literal repository-relative paths."""

from dataclasses import dataclass
from pathlib import Path

from . import git as gitcmd


@dataclass(frozen=True)
class StatusEntry:
    path: str
    index_status: str
    worktree_status: str
    original_path: str | None = None


@dataclass(frozen=True)
class IndexChange:
    path: str
    old_mode: str
    new_mode: str
    status: str


def _query(repo: str | Path, *args: str, index_file: Path | None = None) -> str:
    result = gitcmd.git(repo, *args, raw=True, index_file=index_file)
    if not result.ok:
        raise OSError(f"cannot read Git {args[0]}: {result.err}")
    return result.out


def status_entries(repo: str | Path) -> list[StatusEntry]:
    """Status of each changed/untracked file, excluding ignored files.

    Rename/copy entries retain both paths. Failures raise; an empty list means clean.
    Untracked directories are expanded so callers can select individual files.
    """
    records = iter(
        _query(
            repo, "--no-optional-locks", "status", "--porcelain=v1", "-z", "--untracked-files=all"
        ).split("\0")
    )
    entries = []
    for record in records:
        if not record:
            continue
        if len(record) < 4 or record[2] != " ":
            raise OSError("invalid Git status entry")
        original = None
        if "R" in record[:2] or "C" in record[:2]:
            original = next(records, "")
            if not original:
                raise OSError("missing Git rename/copy source")
        entries.append(StatusEntry(record[3:], record[0], record[1], original))
    return entries


def deleted_tracked_paths(repo: str | Path) -> list[str]:
    """Tracked paths absent from the working tree; failed reads raise."""
    return list(
        dict.fromkeys(
            p for p in _query(repo, "ls-files", "--deleted", "--full-name", "-z").split("\0") if p
        )
    )


def staged_changes(repo: str | Path, *, index_file: Path | None = None) -> list[IndexChange]:
    """Index changes against HEAD (the empty tree for an unborn branch).

    Renames are represented as deletion/addition, so each path retains its own
    old/new mode. This also preserves gitlink removals and type changes.
    """
    records = iter(
        _query(
            repo,
            "diff",
            "--cached",
            "--raw",
            "--no-renames",
            "--no-ext-diff",
            "-z",
            "--",
            index_file=index_file,
        ).split("\0")
    )
    entries = []
    for record in records:
        if not record:
            continue
        fields = record.split()
        path = next(records, "")
        if len(fields) != 5 or not fields[0].startswith(":") or not path:
            raise OSError("invalid Git index change")
        entries.append(IndexChange(path, fields[0][1:], fields[1], fields[4]))
    return entries


def registered_submodules(repo: str | Path) -> list[str]:
    """Paths declared by the working-tree .gitmodules, without initializing them.

    An absent file or absent declarations means no registrations. Invalid config
    and failed reads raise rather than presenting unregistered paths as known.
    """
    root = _query(repo, "rev-parse", "--show-toplevel").removesuffix("\n")
    manifest = Path(root) / ".gitmodules"
    if not manifest.exists():
        return []
    result = gitcmd.git(
        repo,
        "config",
        "--null",
        "--file",
        str(manifest),
        "--get-regexp",
        r"^submodule\..*\.path$",
        raw=True,
    )
    if result.rc == 1:  # git config distinguishes no matching keys from read errors.
        return []
    if not result.ok:
        raise OSError(result.err or "cannot read submodule registrations")
    return [record.split("\n", 1)[1] for record in result.out.split("\0") if record]
