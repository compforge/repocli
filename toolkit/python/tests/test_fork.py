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
    with patch.object(target.c, "post", return_value=rows[1]) as post:
        target.create(source_branch="fix", target_branch="main", title="Fix")
        body = post.call_args.args[1]
        assert body["head"] == "team:fix"
        assert body["head_repo"] == "fork"
        assert "/repos/team/base" in target.c.base_url


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
