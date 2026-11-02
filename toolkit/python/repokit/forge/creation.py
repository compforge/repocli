"""Resolve and create a pull request using the existing Forge operation contract."""

from __future__ import annotations

import logging
from dataclasses import dataclass

from harness_common import Repository

from .model import Forge, ForgeError, PullRequest

logger = logging.getLogger(__name__)


@dataclass(frozen=True)
class PullRequestCreation:
    pull_request: PullRequest
    created: bool
    source_repository: Repository
    target_repository: Repository


def ensure_pull_request(
    forge: Forge,
    *,
    source_branch: str,
    title: str,
    body: str = "",
    target_repository: str | None = None,
    target_branch: str | None = None,
) -> PullRequestCreation:
    """Reuse an unambiguous open PR, or create it in the explicit destination.

    @spec Omitted destination means the source repository, never its fork parent.
    Omitted base preserves an existing PR's base; only creation resolves the default.
    Reuse does not edit title/body. Ambiguous reads and uncertain writes propagate;
    query-before-create is not an exactly-once guarantee across concurrent callers.
    @link component://docs/pr.md
    """
    if not source_branch or target_branch == "":
        raise ValueError("pull request branches must be nonempty")
    source = forge.repository()
    destination = source
    if target_repository is not None:
        name, separator, path = target_repository.partition("/")
        if not separator or not path or name != source.forge.name:
            raise ValueError(
                f"target repository {target_repository!r} must be a Repository ID "
                f"within Forge {source.forge.name!r}"
            )
        destination = Repository(source.forge, path)
    client = forge if destination.id() == source.id() else forge.for_repository(destination.path)
    candidates = [
        pr
        for pr in client.prs_for_branch(source_branch, source_repository=source)
        if pr.is_open and (target_branch is None or pr.target_branch == target_branch)
    ]
    if len(candidates) > 1:
        raise ForgeError(
            f"multiple open pull requests for {source.id()}:{source_branch} "
            f"in {destination.id()}: {[pr.number for pr in candidates]}"
        )
    if candidates:
        pr = candidates[0]
        created = False
    else:
        base = target_branch or client.default_branch()
        if not base:
            raise ForgeError(f"cannot determine default branch for {destination.id()}")
        pr = client.create(
            source_repository=source,
            source_branch=source_branch,
            target_repository=destination,
            target_branch=base,
            title=title,
            body=body,
        )
        created = True
    logger.info(
        "pull request %s source=%s:%s target=%s:%s number=%s",
        "created" if created else "reused",
        source.id(),
        source_branch,
        destination.id(),
        pr.target_branch,
        pr.number,
    )
    return PullRequestCreation(pr, created, source, destination)
