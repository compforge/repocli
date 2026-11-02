"""Working-tree content identities compatible with the Go snapshot digest."""

from __future__ import annotations

import hashlib
import os
import posixpath
import stat
from collections.abc import Mapping
from dataclasses import dataclass, replace
from pathlib import Path
from threading import Event

from ._process import Budget, run
from .git._catalog import Git
from .model import Layout


@dataclass(frozen=True)
class SnapshotScope:
    """Literal repository-relative files/directories, with optional subtree exclusions.

    +spec=`A scope describes captured content, not dependency completeness`
    +link=component://docs/snapshot.md
    """

    paths: tuple[str, ...]
    excludes: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        for paths in (self.paths, self.excludes):
            for path in paths:
                if not path or posixpath.isabs(path) or posixpath.normpath(path) != path:
                    raise ValueError(
                        f"scope path must be normalized and repository-relative: {path!r}"
                    )
                if path == ".." or path.startswith("../"):
                    raise ValueError(f"scope path escapes repository: {path!r}")
        object.__setattr__(self, "paths", tuple(sorted(set(self.paths))))
        object.__setattr__(self, "excludes", tuple(sorted(set(self.excludes))))

    def contains(self, path: str) -> bool:
        def under(root: str) -> bool:
            return root == "." or path == root or path.startswith(root + "/")

        return any(under(root) for root in self.paths) and not any(
            under(root) for root in self.excludes
        )


@dataclass(frozen=True)
class ScopedSnapshot:
    name: str
    digest: str
    file_count: int
    complete: bool
    diagnostics: tuple[str, ...] = ()


def component_scopes(layout: Layout) -> dict[str, SnapshotScope]:
    """Project deepest-owner boundaries without discovering or reading the repository."""
    return {
        component.root: SnapshotScope(
            (component.root,),
            tuple(
                child.root
                for child in layout.components
                if child.root != component.root
                and (component.root == "." or child.root.startswith(component.root + "/"))
            ),
        )
        for component in layout.components
    }


@dataclass(frozen=True)
class Snapshot:
    checkout: str
    digest: str
    file_count: int
    complete: bool
    diagnostics: tuple[str, ...] = ()
    scopes: tuple[ScopedSnapshot, ...] = ()


def snapshot(
    repository: str | Path,
    *,
    timeout: float = 30,
    cancel: Event | None = None,
    scopes: Mapping[str, SnapshotScope] | None = None,
) -> Snapshot:
    """Capture twice to detect concurrent changes; never execute source or follow external links.

    Working-tree input only. Compare identities only when complete is true.
    """
    budget = Budget(timeout, cancel)
    root = Path(
        run(Path(repository), ["rev-parse", "--show-toplevel"], budget).decode().removesuffix("\n")
    ).resolve()
    selected = dict(sorted((scopes or {}).items()))
    first = _capture(root, budget, selected)
    second = _capture(root, budget, selected)
    if first != second:
        return Snapshot(
            str(root),
            second.digest,
            second.file_count,
            False,
            (*second.diagnostics, "snapshot_changed"),
            tuple(
                replace(item, complete=False, diagnostics=(*item.diagnostics, "snapshot_changed"))
                for item in second.scopes
            ),
        )
    return second


def _capture(root: Path, budget: Budget, scopes: Mapping[str, SnapshotScope]) -> Snapshot:
    entries: dict[str, tuple[str, str]] = {}
    output = run(root, ["ls-files", "--stage", "-z"], budget)
    for raw in output.split(b"\0"):
        if not raw:
            continue
        meta, raw_name = raw.split(b"\t", 1)
        mode, raw_oid, stage = meta.split()
        if stage != b"0":
            raise ValueError("unmerged index")
        entries[os.fsdecode(raw_name)] = (mode.decode(), raw_oid.decode())
    for raw in run(root, ["ls-files", "--others", "--exclude-standard", "-z"], budget).split(b"\0"):
        if raw and not raw.endswith(b"/"):
            entries.setdefault(os.fsdecode(raw), ("", ""))
    if len(entries) > 10_000:
        raise ValueError("repository exceeds 10000 files")
    digest = hashlib.sha256(b"repocli-snapshot-v2\0")
    hashes = {name: hashlib.sha256(b"repocli-scoped-snapshot-v1\0") for name in scopes}
    for name, scope in scopes.items():
        for kind, paths in (("include", scope.paths), ("exclude", scope.excludes)):
            for selected_path in paths:
                hashes[name].update(_frame(kind, os.fsencode(selected_path)))

    def add(name: str, record: bytes) -> None:
        digest.update(record)
        for key, scope in scopes.items():
            if scope.contains(name):
                hashes[key].update(record)

    files: set[str] = set()
    links: dict[str, str] = {}
    modules: dict[str, str] = {}
    large: dict[str, str] = {}
    issues: list[str] = []
    total = 0
    for name in sorted(entries, key=os.fsencode):
        budget.remaining()
        path = root / name
        if path.parent.resolve() != path.parent:
            issues.append(name + ": symlinked parent")
            continue
        if entries[name][0] == "160000":
            modules[name] = Git(root, budget).gitlink_oid(name, entries[name][1])
            continue
        try:
            info = path.lstat()
        except FileNotFoundError:
            continue
        if stat.S_ISLNK(info.st_mode):
            links[name] = os.readlink(path)
        elif stat.S_ISREG(info.st_mode):
            fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
            try:
                if not stat.S_ISREG(os.fstat(fd).st_mode):
                    raise ValueError("file changed kind during snapshot")
                if info.st_size <= 2 << 20:
                    total += info.st_size
                    if total > 128 << 20:
                        raise ValueError("snapshot exceeds 128 MiB")
                    content = bytearray()
                    while chunk := os.read(fd, 65536):
                        budget.remaining()
                        content.extend(chunk)
                        if len(content) > 2 << 20:
                            raise ValueError("file grew during snapshot")
                    add(name, _frame(name, bytes(content)))
                else:
                    h = hashlib.sha256()
                    size = 0
                    while chunk := os.read(fd, 65536):
                        budget.remaining()
                        h.update(chunk)
                        size += len(chunk)
                    large[name] = f"{size}:sha256:{h.hexdigest()}"
                files.add(name)
            finally:
                os.close(fd)
        else:
            issues.append(name + ": unsupported file kind")
    resolved_links: dict[str, str] = {}
    for name in links:
        current, seen = name, set()
        while current in links:
            target = links[current]
            if current in seen or posixpath.isabs(target):
                current = ""
                break
            seen.add(current)
            current = posixpath.normpath(posixpath.join(posixpath.dirname(current), target))
            if current == ".." or current.startswith("../"):
                current = ""
                break
        resolved_links[name] = current
        captured = current in files or current in modules
        captured = (
            captured
            or bool(current)
            and any(current == "." or f.startswith(current + "/") for f in files)
        )
        if not captured:
            issues.append(name + ": symlink target is outside captured contents or cyclic/missing")
    for kind, values in (("symlink", links), ("gitlink", modules), ("large_file", large)):
        for name in sorted(values, key=os.fsencode):
            add(name, kind.encode() + b":" + _frame(name, os.fsencode(values[name])))
    for issue in sorted(issues):
        data = issue.encode()
        digest.update(f"issue:{len(data)}:".encode() + data)
    projected = []
    for key, scope in scopes.items():
        scope_issues = list(issues)
        for selected_path in scope.paths:
            included = selected_path == "." or any(
                path == selected_path or path.startswith(selected_path + "/")
                for path in (*files, *links, *modules)
            )
            if not included and os.path.lexists(root / selected_path):
                scope_issues.append(
                    selected_path + ": requested input exists outside captured contents"
                )
        for name, target in resolved_links.items():
            if not scope.contains(name):
                continue
            targets = (
                [target]
                if target in files or target in modules
                else [
                    path
                    for path in (*files, *modules)
                    if target == "." or path.startswith(target + "/")
                ]
            )
            if not targets or any(not scope.contains(path) for path in targets):
                scope_issues.append(name + ": symlink target is outside scope")
        for issue in sorted(scope_issues):
            hashes[key].update(_frame("issue", issue.encode()))
        projected.append(
            ScopedSnapshot(
                key,
                "sha256:" + hashes[key].hexdigest(),
                sum(scope.contains(path) for path in files),
                not scope_issues,
                tuple(scope_issues),
            )
        )
    return Snapshot(
        str(root),
        "sha256:" + digest.hexdigest(),
        len(files),
        not issues,
        tuple(issues),
        tuple(projected),
    )


def _frame(name: str, data: bytes) -> bytes:
    path = os.fsencode(name)
    return str(len(path)).encode() + b":" + path + str(len(data)).encode() + b":" + data
