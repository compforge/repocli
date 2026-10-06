"""One operation owns input selection, deadline and consistency observation."""

import math
import os
from pathlib import Path
from threading import Event

from ._git import Git
from ._layout import load
from ._process import Budget
from .model import Diagnostic, InspectReport


def inspect(
    repository: str | os.PathLike[str] = ".",
    *,
    head: str | None = None,
    staged: bool = False,
    timeout: float = 5.0,
    cancel: Event | None = None,
) -> InspectReport:
    """Inspect organization using Git paths and layout metadata only.

    timeout is a whole-call budget in seconds; cancel accepts a threading.Event.
    Git processes are killed and reaped on timeout/cancellation. Filesystem reads and
    Python projection check the budget between operations. Concurrent catalog changes
    return complete=False; consumers decide whether to accept the observation.
    """
    if head and staged:
        raise ValueError("head and staged are mutually exclusive")
    if not math.isfinite(timeout) or timeout <= 0:
        raise ValueError("timeout must be positive and finite")
    budget = Budget(timeout, cancel)
    budget.remaining()
    start = Path(repository).resolve(strict=True)
    initial = Git(start, budget)
    root = Path(
        os.fsdecode(initial.run(["rev-parse", "--show-toplevel"])).removesuffix("\n")
    ).resolve()
    git = Git(root, budget)
    commit = (
        git.run(["rev-parse", "--verify", "--end-of-options", head + "^{commit}"]).decode().strip()
        if head
        else ""
    )
    files = git.catalog(commit, staged)
    origin = (
        git.run(["config", "--get", "remote.origin.url"], allow_missing=True)
        .decode("utf-8", errors="replace")
        .rstrip("\n")
    )
    layout = load(files, origin)
    complete = bool(commit) or files == git.catalog(commit, staged)
    budget.remaining()
    return InspectReport(
        repository=layout.repository,
        components=layout.components,
        checkout=str(root),
        input="commit" if commit else "index" if staged else "working_tree",
        head=commit or None,
        complete=complete,
        diagnostics=()
        if complete
        else (
            Diagnostic(
                "inspection_changed",
                "repository paths or layout metadata changed during inspection",
            ),
        ),
    )
