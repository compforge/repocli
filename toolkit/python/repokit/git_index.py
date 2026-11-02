"""Working-tree and index facts, preserving literal repository-relative paths."""

from dataclasses import dataclass, field
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


@dataclass(frozen=True)
class IndexEntry:
    """One Git index entry; mode and oid retain Git's meaning, including gitlinks."""

    path: str
    mode: str
    oid: str
    stage: int


@dataclass(frozen=True)
class IndexView:
    """Captured index entries and changes, with config queries bound to their blob IDs.

    Gitlinks are entries like any other path. Registration and commit acceptance
    belong to consumers. Queries never fall back to working-tree files.
    """

    changes: tuple[IndexChange, ...]
    entries: tuple[IndexEntry, ...]
    _repo: Path = field(repr=False)

    def config_values(self, path: str, pattern: str) -> list[str]:
        """Read matching Git-config values from a regular file in this index.

        Missing paths or keys return an empty list. Unmerged/non-file entries,
        malformed config and failed queries raise. Includes are not followed.
        """
        entries = [entry for entry in self.entries if entry.path == path]
        if not entries:
            return []
        if len(entries) != 1 or entries[0].stage != 0:
            raise OSError(f"unmerged config entry: {path}")
        entry = entries[0]
        if entry.mode not in ("100644", "100755"):
            raise OSError(f"config entry is not a regular file: {path}")
        result = gitcmd.git(
            self._repo,
            "config",
            "--no-includes",
            "--null",
            "--blob",
            entry.oid,
            "--get-regexp",
            pattern,
            raw=True,
        )
        # Git's blob reader also returns 1 for malformed config, with stderr.
        # Only a clean no-match result means an empty selection.
        if result.rc == 1 and not result.err:
            return []
        if not result.ok:
            raise OSError(result.err or f"cannot read index config: {path}")
        return [record.partition("\n")[2] for record in result.out.split("\0") if record]


def index_view(repo: str | Path, *, index_file: Path | None = None) -> IndexView:
    """Capture index facts; callers must serialize writes while this is collected."""
    entries = []
    for record in _query(
        repo, "ls-files", "--stage", "--full-name", "-z", index_file=index_file
    ).split("\0"):
        if not record:
            continue
        metadata, separator, path = record.partition("\t")
        fields = metadata.split()
        if not separator or not path or len(fields) != 3 or fields[2] not in ("0", "1", "2", "3"):
            raise OSError("invalid Git index entry")
        entries.append(IndexEntry(path, fields[0], fields[1], int(fields[2])))
    return IndexView(
        tuple(staged_changes(repo, index_file=index_file)), tuple(entries), Path(repo).resolve()
    )


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
