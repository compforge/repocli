"""Versioned repository entries, independent of engineering layout configuration."""

from __future__ import annotations

import os
import posixpath
import stat
from dataclasses import dataclass
from pathlib import Path
from threading import Event
from typing import Literal

from ._git import Git
from ._layout import valid_path
from ._process import Budget
from .model import Diagnostic


@dataclass(frozen=True, slots=True)
class Directory:
    """A path implied by repository entries; includes the root, represented by '.'."""

    path: str


@dataclass(frozen=True, slots=True)
class Manifest:
    """Project metadata, whether or not its directory becomes a Component."""

    path: str
    ecosystem: str


@dataclass(frozen=True, slots=True)
class File:
    path: str
    kind: Literal["regular", "symlink", "gitlink"]
    mode: str
    oid: str | None = None
    role: str | None = None
    manifest: Manifest | None = None


@dataclass(frozen=True, slots=True)
class TreeReport:
    checkout: str
    input: Literal["working_tree", "index", "commit"]
    directories: tuple[Directory, ...]
    files: tuple[File, ...]
    complete: bool
    head: str | None = None
    diagnostics: tuple[Diagnostic, ...] = ()


def tree(
    repository: str | Path = ".",
    *,
    head: str | None = None,
    staged: bool = False,
    timeout: float = 5,
    cancel: Event | None = None,
) -> TreeReport:
    """List tracked/nonignored entries, without source reads or Component discovery.

    Empty directories are outside Git's catalog. Working regular files have no OID;
    gitlinks use checkout HEAD when initialized and the index reference otherwise.
    Never enter child repository contents.
    """
    if head and staged:
        raise ValueError("head and staged are mutually exclusive")
    budget = Budget(timeout, cancel)
    initial = Git(Path(repository), budget)
    root = Path(
        os.fsdecode(initial.run(["rev-parse", "--show-toplevel"])).removesuffix("\n")
    ).resolve()
    git = Git(root, budget)
    commit = (
        git.run(["rev-parse", "--verify", "--end-of-options", head + "^{commit}"]).decode().strip()
        if head
        else ""
    )
    files = _entries(git, commit, staged)
    complete = bool(commit) or files == _entries(git, commit, staged)
    directories = {"."}
    for file in files:
        parent = posixpath.dirname(file.path)
        while parent:
            directories.add(parent)
            parent = posixpath.dirname(parent)
    return TreeReport(
        str(root),
        "commit" if commit else "index" if staged else "working_tree",
        tuple(Directory(p) for p in sorted(directories, key=os.fsencode)),
        files,
        complete,
        commit or None,
        ()
        if complete
        else (Diagnostic("tree_changed", "repository entries changed during observation"),),
    )


def _entries(git: Git, head: str, staged: bool) -> tuple[File, ...]:
    from ._layout import manifest_ecosystem

    committed = bool(head) or staged
    output = git.run(
        ["ls-tree", "-r", "-z", "--full-tree", head] if head else ["ls-files", "--stage", "-z"]
    )
    entries: dict[str, tuple[str, str]] = {}
    for raw in output.split(b"\0"):
        if not raw:
            continue
        meta, sep, name = raw.partition(b"\t")
        parts = meta.split()
        path = os.fsdecode(name)
        if not sep or len(parts) != 3 or not valid_path(path):
            raise ValueError("invalid Git entry")
        if not head and parts[2] != b"0":
            raise ValueError(f"unmerged index entry: {path}")
        entries[path] = (parts[0].decode(), parts[2 if head else 1].decode())
    if not committed:
        for raw in git.run(["ls-files", "--others", "--exclude-standard", "-z"]).split(b"\0"):
            if raw and not raw.endswith(b"/"):
                path = os.fsdecode(raw)
                if not valid_path(path):
                    raise ValueError("unsafe repository path")
                entries.setdefault(path, ("", ""))
    if len(entries) > 10_000:
        raise ValueError("repository exceeds 10000 files")
    files = []
    for path, (mode, oid) in sorted(entries.items(), key=lambda item: os.fsencode(item[0])):
        git.budget.remaining()
        if not committed and mode != "160000":
            full = git.root / path
            if full.parent.resolve() != full.parent:
                raise ValueError(f"symlinked directory: {path}")
            try:
                info = full.lstat()
            except FileNotFoundError:
                continue
            if stat.S_ISLNK(info.st_mode):
                mode = "120000"
            elif stat.S_ISREG(info.st_mode):
                mode = "100755" if info.st_mode & 0o111 else "100644"
            else:
                raise ValueError(f"unsupported entry: {path}")
            oid = ""
        if not committed and mode == "160000":
            oid = git.gitlink_oid(path, oid)
        kinds: dict[str, Literal["regular", "symlink", "gitlink"]] = {
            "100644": "regular",
            "100755": "regular",
            "120000": "symlink",
            "160000": "gitlink",
        }
        if mode not in kinds:
            raise ValueError(f"unsupported Git mode: {mode}")
        ecosystem = manifest_ecosystem(path) if kinds[mode] == "regular" else ""
        manifest = Manifest(path, ecosystem) if ecosystem else None
        role = "manifest" if manifest else _role(path) if kinds[mode] == "regular" else None
        files.append(File(path, kinds[mode], mode, oid or None, role, manifest))
    return tuple(files)


def _role(path: str) -> str | None:
    name = posixpath.basename(path)
    if name in (
        "Makefile",
        "makefile",
        "GNUmakefile",
        "CMakeLists.txt",
        "build.gradle",
        "build.gradle.kts",
    ):
        return "build_script"
    if name in (
        "go.sum",
        "uv.lock",
        "poetry.lock",
        "Pipfile.lock",
        "Cargo.lock",
        "package-lock.json",
        "npm-shrinkwrap.json",
        "pnpm-lock.yaml",
        "yarn.lock",
        "bun.lock",
        "bun.lockb",
    ):
        return "lockfile"
    return None
