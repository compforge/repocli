"""Read Git catalogs and layout metadata without reading source contents."""

import os
import stat
from pathlib import Path

from ._layout import Catalog, needs_content, valid_path
from ._process import Budget, run

MAX_FILE_BYTES = 2 << 20
MAX_TOTAL_BYTES = 128 << 20
MAX_FILES = 10_000


class Git:
    def __init__(self, root: Path, budget: Budget):
        self.root = root
        self.budget = budget

    def run(
        self,
        args: list[str],
        *,
        input: bytes = b"",
        allow_missing: bool = False,
        max_output: int = 16 << 20,
    ) -> bytes:
        return run(
            self.root,
            args,
            self.budget,
            input=input,
            allow_missing=allow_missing,
            max_output=max_output,
        )

    def gitlink_oid(self, name: str, oid: str) -> str:
        """Read checkout HEAD only; uninitialized entries retain the index reference."""
        path = self.root / name
        try:
            (path / ".git").lstat()
        except FileNotFoundError:
            return oid
        if path.resolve() != path:
            raise ValueError(f"symlinked gitlink checkout: {name}")
        child = Git(path, self.budget)
        root = os.fsdecode(child.run(["rev-parse", "--show-toplevel"])).removesuffix("\n")
        if Path(root) != path:
            raise ValueError(f"invalid gitlink checkout: {name}")
        return child.run(["rev-parse", "--verify", "HEAD"]).decode().strip()

    def catalog(self, head: str, staged: bool) -> Catalog:
        committed = bool(head) or staged
        output = self.run(
            ["ls-tree", "-r", "-z", "--full-tree", head] if head else ["ls-files", "--stage", "-z"]
        )
        entries: dict[str, str] = {}
        for entry in output.split(b"\0"):
            if not entry:
                continue
            fields, separator, raw_name = entry.partition(b"\t")
            parts = fields.split()
            name = os.fsdecode(raw_name)
            if not separator or len(parts) != 3 or not valid_path(name):
                raise ValueError("invalid Git catalog entry")
            mode = parts[0]
            if not head and parts[2] != b"0":
                raise ValueError(f"unmerged index entry: {name}")
            if mode == b"160000":
                continue
            if mode == b"120000" and committed:
                if needs_content(name):
                    raise ValueError(f"metadata {name} is a symlink")
                continue
            if mode not in (b"100644", b"100755", b"120000"):
                raise ValueError(f"unsupported Git entry mode for {name}")
            entries[name] = parts[2 if head else 1].decode("ascii")
        if not committed:
            for raw_name in self.run(["ls-files", "-z", "--others", "--exclude-standard"]).split(
                b"\0"
            ):
                name = os.fsdecode(raw_name)
                if not name or name.endswith("/"):
                    continue
                if not valid_path(name):
                    raise ValueError("unsafe repository path")
                entries[name] = ""
        if len(entries) > MAX_FILES:
            raise ValueError("repository exceeds 10000 files")
        files: Catalog = {}
        selected: list[tuple[str, str]] = []
        total = 0
        for name in sorted(entries):
            self.budget.remaining()
            if committed:
                files[name] = None
                if needs_content(name):
                    oid = entries[name]
                    selected.append((name, oid))
                continue
            path = self.root / name
            try:
                info = path.lstat()
            except FileNotFoundError:
                continue
            if not stat.S_ISREG(info.st_mode):
                if needs_content(name):
                    raise ValueError(f"metadata {name} is not a regular file")
                continue
            if path.parent.resolve() != path.parent:
                if needs_content(name):
                    raise ValueError(f"metadata {name} has a symlinked directory")
                continue
            files[name] = None
            if not needs_content(name):
                continue
            if info.st_size > MAX_FILE_BYTES:
                raise ValueError(f"metadata {name} exceeds {MAX_FILE_BYTES} bytes")
            fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
            try:
                if not stat.S_ISREG(os.fstat(fd).st_mode):
                    raise ValueError(f"metadata {name} is not a regular file")
                content = bytearray()
                while len(content) <= MAX_FILE_BYTES:
                    self.budget.remaining()
                    chunk = os.read(fd, min(65536, MAX_FILE_BYTES + 1 - len(content)))
                    if not chunk:
                        break
                    content.extend(chunk)
                if len(content) > MAX_FILE_BYTES:
                    raise ValueError(f"metadata {name} exceeds {MAX_FILE_BYTES} bytes")
                total += len(content)
                if total > MAX_TOTAL_BYTES:
                    raise ValueError("metadata exceeds 128 MiB")
                files[name] = bytes(content)
            finally:
                os.close(fd)
        if selected:
            self._blobs(selected, files)
        return files

    def _blobs(self, selected: list[tuple[str, str]], files: Catalog) -> None:
        batch = ("\n".join(oid for _, oid in selected) + "\n").encode("ascii")
        headers = self.run(["cat-file", "--batch-check"], input=batch).splitlines()
        if len(headers) != len(selected):
            raise ValueError("invalid Git blob batch")
        sizes: list[int] = []
        total = 0
        # Check immutable object sizes before reading any metadata contents.
        for (name, oid), header in zip(selected, headers, strict=True):
            fields = header.split()
            if len(fields) != 3 or fields[:2] != [oid.encode("ascii"), b"blob"]:
                raise ValueError("invalid Git blob")
            size = int(fields[2])
            if size < 0:
                raise ValueError("invalid Git blob size")
            if size > MAX_FILE_BYTES:
                raise ValueError(f"metadata {name} exceeds {MAX_FILE_BYTES} bytes")
            total += size
            if total > MAX_TOTAL_BYTES:
                raise ValueError("metadata exceeds 128 MiB")
            sizes.append(size)
        data = self.run(
            ["cat-file", "--batch"], input=batch, max_output=total + len(selected) * 128
        )
        offset = 0
        for (name, oid), size in zip(selected, sizes, strict=True):
            end = data.find(b"\n", offset)
            if end < 0 or data[offset:end] != f"{oid} blob {size}".encode("ascii"):
                raise ValueError("invalid Git blob header")
            offset = end + 1
            if data[offset + size : offset + size + 1] != b"\n":
                raise ValueError("truncated Git blob")
            files[name] = data[offset : offset + size]
            offset += size + 1
        if offset != len(data):
            raise ValueError("unexpected Git blob bytes")
