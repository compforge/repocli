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
    target = GitHubForge("github.com", "team", "base", "token").for_repository(
        "team/base", source_repository="team/fork"
    )
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
        assert [p.number for p in target.prs_for_branch("fix")] == [12]
        assert query.call_args.kwargs["head"] == "team:fix"


@pytest.mark.parametrize(
    ("target_path", "source_path", "expected_head"),
    [
        ("team/base", "team/fork", "team:fix"),
        ("upstream/base", "contributor/fork", "contributor:fix"),
    ],
    ids=["same-owner", "cross-owner"],
)
def test_github_fork_creation_posts_to_target_with_source_head(
    target_path, source_path, expected_head
):
    """Keep the PR endpoint's owner independent from the head branch's owner.

    Contract: https://docs.github.com/en/rest/pulls/pulls#create-a-pull-request
    """
    target = GitHubForge("github.com", "team", "base", "token").for_repository(
        target_path, source_repository=source_path
    )
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
            source_branch="fix", target_branch="main", title="Fix", body="Description"
        )
    send.assert_called_once()
    request = send.call_args.args[0]
    assert request.method == "POST"
    assert request.full_url == f"https://api.github.com/repos/{target_path}/pulls"
    assert json.loads(request.data) == {
        "head": expected_head,
        "head_repo": "fork",
        "base": "main",
        "title": "Fix",
        "body": "Description",
    }
    assert pr.source_repository == source_path
    assert pr.target_repository == target_path


def test_gitlab_fork_creation_posts_to_source_but_query_uses_target():
    client = GitLabForge("gitlab.example", "parent/r", "token", source_repository="fork/r")
    calls = []

    def request(connection, method, path, *, params=None, body=None):
        calls.append((connection.base_url, method, path, params, body))
        if method == "POST":
            return {"iid": 42, "state": "opened"}
        return {"id": 7 if "parent%2Fr" in connection.base_url else 8}

    with patch.object(RestClient, "request", request):
        pr = client.create(source_branch="fix", target_branch="main", title="Fix")
    assert pr.target_repository == "parent/r"
    assert pr.source_repository == "fork/r"
    assert calls[-1][0].endswith("fork%2Fr")
    assert calls[-1][-1]["target_project_id"] == 7
    rows = [{"iid": 41, "source_project_id": 9}, {"iid": 42, "source_project_id": 8}]
    with (
        patch.object(RestClient, "request", request),
        patch.object(client.c, "get_all", return_value=rows),
    ):
        assert [p.number for p in client.prs_for_branch("fix")] == [42]
