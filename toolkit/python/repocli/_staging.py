"""Prepare and validate an index while holding Git's native writer lock."""

import os
import shutil
import tempfile
from collections.abc import Callable
from pathlib import Path

from .git import GitResult, git
from .git_index import IndexView, index_view
from .git_state import git_path


def stage_validated(
    repo: str | Path, paths: list[str], validate: Callable[[IndexView], None]
) -> GitResult:
    index = git_path(repo, "index")
    lock = index.with_name(index.name + ".lock")
    try:
        lock_file = lock.open("xb")
    except OSError as exc:
        return GitResult(-1, "", f"cannot lock Git index: {exc}")
    installed = False
    try:
        # why: hold the native lock from snapshot through replacement, so another
        # Git writer cannot have its index update silently overwritten.
        with (
            lock_file,
            tempfile.TemporaryDirectory(prefix="repocli-index-", dir=index.parent) as tmp,
        ):
            prepared = Path(tmp) / "index"
            if index.exists():
                shutil.copy2(index, prepared)
            result = GitResult(0, "", "")
            if paths:
                result = git(
                    repo,
                    "--literal-pathspecs",
                    "add",
                    "--",
                    *paths,
                    timeout=30,
                    index_file=prepared,
                )
                if not result.ok:
                    return result
            validate(index_view(repo, index_file=prepared))
            if prepared.exists():
                # Use the lock file as the replacement, following Git's index protocol.
                with prepared.open("rb") as source:
                    shutil.copyfileobj(source, lock_file)
                lock_file.flush()
                os.fchmod(lock_file.fileno(), prepared.stat().st_mode & 0o777)
                os.replace(lock, index)
                installed = True
            return result
    finally:
        if not installed:
            lock.unlink(missing_ok=True)
