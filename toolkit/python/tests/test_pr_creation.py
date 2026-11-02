from unittest.mock import Mock

import pytest
from harness_common import Forge as ForgeIdentity
from harness_common import Repository

from repokit.forge import ensure_pull_request
from repokit.forge.model import Forge, ForgeError, ForgeOutcomeUnknown, PullRequest


def client(path="fork/repo", prs=()):
    forge = Mock(spec=Forge)
    forge.repository.return_value = Repository(
        ForgeIdentity("github"), path, forked_from="upstream/repo"
    )
    forge.prs_for_branch.return_value = list(prs)
    forge.default_branch.return_value = "trunk"
    forge.create.side_effect = lambda **kw: PullRequest(
        99,
        state="open",
        source_branch=kw["source_branch"],
        target_branch=kw["target_branch"],
        source_repository=kw["source_repository"].path,
        target_repository=kw["target_repository"].path,
    )
    return forge


def test_current_repository_is_default_even_for_fork():
    forge = client()
    result = ensure_pull_request(forge, source_branch="fix", title="title", body="body")
    assert result.created and result.pull_request.target_branch == "trunk"
    assert result.source_repository == result.target_repository
    forge.for_repository.assert_not_called()
    assert forge.create.call_args.kwargs["body"] == "body"
    forge.prs_for_branch.assert_called_once_with("fix", source_repository=result.source_repository)


@pytest.mark.parametrize("base", [None, "release", "trunk"])
def test_explicit_destination_and_base_preserve_reuse(base):
    """@case An omitted target preserves an existing nondefault base without metadata fallback."""
    existing = PullRequest(8, state="open", source_branch="fix", target_branch="release")
    source, destination = client(), client("upstream/repo", [existing])
    source.for_repository.return_value = destination
    result = ensure_pull_request(
        source,
        source_branch="fix",
        target_repository="github/upstream/repo",
        target_branch=base,
        title="title",
        body="new text",
    )
    assert result.created == (base == "trunk")
    assert result.pull_request.target_branch == (base or "release")
    destination.default_branch.assert_not_called()
    destination.update.assert_not_called()
    source.create.assert_not_called()
    assert result.target_repository.path == "upstream/repo"
    source.for_repository.assert_called_once_with("upstream/repo")
    destination.prs_for_branch.assert_called_once_with(
        "fix", source_repository=result.source_repository
    )


def test_explicit_current_repository_and_destination_without_fork_metadata():
    source = client()
    source.repository.return_value = Repository(ForgeIdentity("github"), "fork/repo")
    result = ensure_pull_request(
        source, source_branch="fix", target_repository="github/fork/repo", title="t"
    )
    assert result.created
    source.for_repository.assert_not_called()
    source.for_repository.return_value = client("another/repo")
    result = ensure_pull_request(
        source, source_branch="fix", target_repository="github/another/repo", title="t"
    )
    assert result.target_repository.path == "another/repo"
    source.for_repository.assert_called_once_with("another/repo")
    source.for_repository.return_value.prs_for_branch.assert_called_once_with(
        "fix", source_repository=result.source_repository
    )


@pytest.mark.parametrize("destination", ["parent", "current", "github/", "gitlab/upstream/repo"])
def test_invalid_destination_never_writes(destination):
    forge = client()
    with pytest.raises(ValueError, match="Repository ID"):
        ensure_pull_request(forge, source_branch="fix", target_repository=destination, title="t")
    forge.create.assert_not_called()


@pytest.mark.parametrize("method", ["repository", "prs_for_branch", "default_branch"])
def test_failed_lookup_never_writes(method):
    forge = client()
    getattr(forge, method).side_effect = ForgeError("offline")
    with pytest.raises(ForgeError, match="offline"):
        ensure_pull_request(forge, source_branch="fix", title="t")
    forge.create.assert_not_called()


def test_ambiguous_open_prs_fail_but_finished_prs_do_not_block_creation():
    forge = client(prs=[PullRequest(n, state="open") for n in (1, 2)])
    with pytest.raises(ForgeError, match="multiple open"):
        ensure_pull_request(forge, source_branch="fix", title="t")
    forge.create.assert_not_called()
    forge.prs_for_branch.return_value = [
        PullRequest(1, state="merged"),
        PullRequest(2, state="closed"),
    ]
    assert ensure_pull_request(forge, source_branch="fix", title="t").created


def test_uncertain_creation_is_never_retried_and_later_observation_can_reuse():
    forge = client()
    forge.create.side_effect = ForgeOutcomeUnknown("response lost")
    with pytest.raises(ForgeOutcomeUnknown):
        ensure_pull_request(forge, source_branch="fix", title="t")
    forge.create.assert_called_once()
    forge.prs_for_branch.return_value = [PullRequest(3, state="open", target_branch="main")]
    assert not ensure_pull_request(forge, source_branch="fix", title="t").created
    forge.create.assert_called_once()
