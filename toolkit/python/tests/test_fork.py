import io
import json
from unittest.mock import patch

import pytest
from harness_common import Forge, Repository

from repocli.forge import GitHubForge, GitLabForge
from repocli.forge._rest import RestClient
from repocli.forge.model import ForgeAuthError


@pytest.mark.parametrize("provider", ["github", "gitlab"])
def test_repository_direct_parent_and_absence(provider):
    client = (
        GitHubForge("github.com", "fork", "r", "token")
        if provider == "github"
        else GitLabForge("gitlab.example", "fork/r", "token")
    )
    data = (
        {
            "full_name": "fork/r",
            "parent": {"full_name": "parent/r"},
            "source": {"full_name": "root/r"},
        }
        if provider == "github"
        else {
            "path_with_namespace": "fork/r",
            "forked_from_project": {"path_with_namespace": "parent/r"},
        }
    )
    with patch.object(client.c, "get", return_value=data):
        result = client.repository()
    assert result == Repository(Forge("github" if provider == "github" else client.host), "fork/r")
    assert result.forked_from == "parent/r"
    data.pop("parent" if provider == "github" else "forked_from_project")
    with patch.object(client.c, "get", return_value=data):
        assert client.repository().forked_from is None
    with patch.object(client.c, "get", side_effect=ForgeAuthError("denied")):
        with pytest.raises(ForgeAuthError):
            client.repository()


def test_github_same_owner_forks_do_not_share_branch_identity():
    target = GitHubForge("github.com", "team", "base", "token")
    source = Repository(Forge("github"), "team/fork")
    rows = [
        {
            "number": n,
            "state": "open",
            "head": {"ref": "fix", "repo": {"full_name": path}},
            "base": {"repo": {"full_name": "team/base"}},
        }
        for n, path in [(11, "team/other"), (12, "team/fork")]
    ]
    with patch.object(target.c, "get_all", return_value=rows) as query:
        assert [p.number for p in target.prs_for_branch("fix", source_repository=source)] == [12]
        assert query.call_args.kwargs["head"] == "team:fix"


@pytest.mark.parametrize(
    ("target_path", "source_path", "expected_head"),
    [
        ("team/base", "team/base", "team:fix"),
        ("team/base", "team/fork", "team:fix"),
        ("upstream/base", "contributor/fork", "contributor:fix"),
    ],
    ids=["same-repository", "same-owner", "cross-owner"],
)
def test_github_fork_creation_posts_to_target_with_source_head(
    target_path, source_path, expected_head
):
    """Keep the PR endpoint's owner independent from the head branch's owner.

    Contract: https://docs.github.com/en/rest/pulls/pulls#create-a-pull-request
    """
    # The explicit tuple overrides the client's unrelated repository scope.
    target = GitHubForge("github.com", "unrelated", "repo", "token")
    response = {
        "number": 42,
        "state": "open",
        "head": {"ref": "fix", "repo": {"full_name": source_path}},
        "base": {"ref": "main", "repo": {"full_name": target_path}},
    }
    with patch(
        "urllib.request.urlopen", return_value=io.BytesIO(json.dumps(response).encode())
    ) as send:
        pr = target.create(
            source_repository=Repository(Forge("github"), source_path),
            source_branch="fix",
            target_repository=Repository(Forge("github"), target_path),
            target_branch="main",
            title="Fix",
            body="Description",
        )
    send.assert_called_once()
    request = send.call_args.args[0]
    assert request.method == "POST"
    assert request.full_url == f"https://api.github.com/repos/{target_path}/pulls"
    assert json.loads(request.data) == {
        "head": expected_head,
        "head_repo": source_path.split("/", 1)[1],
        "base": "main",
        "title": "Fix",
        "body": "Description",
    }
    assert pr.source_repository == source_path
    assert pr.target_repository == target_path


@pytest.mark.parametrize("source_path", ["parent/r", "fork/r"])
def test_gitlab_fork_creation_posts_to_source_but_query_uses_target(source_path):
    client = GitLabForge("gitlab.example", "unrelated/repo", "token")
    source = Repository(Forge("gitlab.example"), source_path)
    target = Repository(source.forge, "parent/r")
    calls = []

    def request(connection, method, path, *, params=None, body=None):
        calls.append((connection.base_url, method, path, params, body))
        if method == "POST":
            return {"iid": 42, "state": "opened"}
        return {"id": 7 if "parent%2Fr" in connection.base_url else 8}

    with patch.object(RestClient, "request", request):
        pr = client.create(
            source_repository=source,
            source_branch="fix",
            target_repository=target,
            target_branch="main",
            title="Fix",
        )
    assert pr.target_repository == "parent/r"
    assert pr.source_repository == source_path
    assert calls[-1][0].endswith(source_path.replace("/", "%2F"))
    assert calls[-1][-1]["target_project_id"] == 7
    client = client.for_repository(target.path)
    source_id = 7 if source_path == "parent/r" else 8
    rows = [{"iid": 41, "source_project_id": 9}, {"iid": 42, "source_project_id": source_id}]
    with (
        patch.object(RestClient, "request", request),
        patch.object(client.c, "get_all", return_value=rows),
    ):
        assert [p.number for p in client.prs_for_branch("fix", source_repository=source)] == [42]


@pytest.mark.parametrize("provider", ["github", "gitlab"])
@pytest.mark.parametrize("wrong_side", ["source", "target"])
def test_create_rejects_cross_forge_before_io(provider, wrong_side):
    """@case A foreign repository cannot route a write through this Forge's credentials."""
    client = (
        GitHubForge("github.com", "owner", "r", "token")
        if provider == "github"
        else GitLabForge("gitlab.com", "owner/r", "token")
    )
    local = Repository(Forge(provider), "owner/r")
    foreign = Repository(Forge("other.example"), "owner/r")
    with patch.object(RestClient, "request") as request:
        with pytest.raises(ValueError, match="does not belong to Forge"):
            client.create(
                source_repository=foreign if wrong_side == "source" else local,
                source_branch="fix",
                target_repository=foreign if wrong_side == "target" else local,
                target_branch="main",
                title="Fix",
            )
        request.assert_not_called()


@pytest.mark.parametrize("provider", ["github", "gitlab"])
def test_branch_query_defaults_to_current_repository(provider):
    client = (
        GitHubForge("github.com", "owner", "r", "token")
        if provider == "github"
        else GitLabForge("gitlab.com", "owner/r", "token")
    )
    rows = (
        [
            {"number": 1, "head": {"repo": {"full_name": "owner/r"}}},
            {"number": 2, "head": {"repo": {"full_name": "other/r"}}},
        ]
        if provider == "github"
        else [{"iid": 1, "source_project_id": 7}, {"iid": 2, "source_project_id": 8}]
    )
    with (
        patch.object(RestClient, "get", return_value={"id": 7}),
        patch.object(client.c, "get_all", return_value=rows),
    ):
        assert [pr.number for pr in client.prs_for_branch("fix")] == [1]
