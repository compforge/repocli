"""Working-tree content identities compatible with the Go snapshot digest."""

from __future__ import annotations

import hashlib
import os
import posixpath
import stat
from dataclasses import dataclass
from pathlib import Path
from threading import Event

from ._process import Budget, run


@dataclass(frozen=True)
class Snapshot:
    checkout: str
    digest: str
    file_count: int
    complete: bool
    diagnostics: tuple[str, ...] = ()


def snapshot(
    repository: str | Path, *, timeout: float = 30, cancel: Event | None = None
) -> Snapshot:
    """Capture twice to detect concurrent changes; never execute source or follow external links.

    Working-tree input only. Compare identities only when complete is true.
    """
    budget = Budget(timeout, cancel)
    root = Path(
        run(Path(repository), ["rev-parse", "--show-toplevel"], budget).decode().strip()
    ).resolve()
    first = _capture(root, budget, 0)
    second = _capture(root, budget, 0)
    if first != second:
        return Snapshot(
            str(root),
            second.digest,
            second.file_count,
            False,
            (*second.diagnostics, "snapshot_changed"),
        )
    return second


def _capture(root: Path, budget: Budget, depth: int) -> Snapshot:
    entries: dict[str, str] = {}
    output = run(root, ["ls-files", "--stage", "-z"], budget)
    for raw in output.split(b"\0"):
        if not raw:
            continue
        meta, raw_name = raw.split(b"\t", 1)
        mode, _, stage = meta.split()
        if stage != b"0":
            raise ValueError("unmerged index")
        entries[os.fsdecode(raw_name)] = mode.decode()
    for raw in run(root, ["ls-files", "--others", "--exclude-standard", "-z"], budget).split(b"\0"):
        if raw and not raw.endswith(b"/"):
            entries.setdefault(os.fsdecode(raw), "")
    if len(entries) > 10_000:
        raise ValueError("repository exceeds 10000 files")
    digest = hashlib.sha256()
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
        try:
            info = path.lstat()
        except FileNotFoundError:
            continue
        if entries[name] == "160000":
            if depth >= 8 or not (path / ".git").exists() or path.resolve() != path:
                issues.append(name + ": submodule unavailable or nesting limit")
                continue
            oid = run(path, ["rev-parse", "HEAD"], budget).decode().strip()
            child = _capture(path, budget, depth + 1)
            modules[name] = oid + ":" + child.digest
            issues.extend(name + "/" + item for item in child.diagnostics)
        elif stat.S_ISLNK(info.st_mode):
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
                    digest.update(_frame(name, bytes(content)))
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
        captured = current in files or any(
            current == m or current.startswith(m + "/") for m in modules
        )
        captured = (
            captured
            or bool(current)
            and any(current == "." or f.startswith(current + "/") for f in files)
        )
        if not captured:
            issues.append(name + ": symlink target is outside captured contents or cyclic/missing")
    for kind, values in (("symlink", links), ("submodule", modules), ("large_file", large)):
        for name in sorted(values, key=os.fsencode):
            digest.update(kind.encode() + b":" + _frame(name, os.fsencode(values[name])))
    for issue in sorted(issues):
        data = issue.encode()
        digest.update(f"issue:{len(data)}:".encode() + data)
    return Snapshot(
        str(root), "sha256:" + digest.hexdigest(), len(files), not issues, tuple(issues)
    )


def _frame(name: str, data: bytes) -> bytes:
    path = os.fsencode(name)
    return str(len(path)).encode() + b":" + path + str(len(data)).encode() + b":" + data
