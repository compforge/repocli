"""Public identities and query results, independent of local storage."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Literal

from harness_common import Repository

from ...git.state import CheckoutEntry
from ..model import ForgeError, PullRequest


@dataclass(frozen=True)
class PullRequestKey:
    repository: Repository
    number: int


@dataclass(frozen=True)
class PullRequestStatus:
    key: PullRequestKey
    state: Literal["open", "closed", "merged"]
    observed_at: str


class PullRequestStatusUnavailable(ForgeError):
    """No current lifecycle observation; an old snapshot is not a successful query."""


@dataclass(frozen=True)
class BranchPullRequests:
    repository: Repository
    branch: str
    head: str
    candidates: tuple[PullRequest, ...]
    checkouts: tuple[CheckoutEntry, ...]
    selected: PullRequest | None
    ambiguous: bool = False
    error: str | None = None
