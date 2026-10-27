"""Repository paging is explicit, bounded and independent of local repository scope."""

import io
import json
import urllib.error
import urllib.request
from dataclasses import asdict
from email.message import Message
from urllib.parse import parse_qs, urlsplit

import pytest

from repocli.forge import GitHubForge, GitLabForge, Page
from repocli.forge.model import ForgeError


class Response:
    status = 200

    def __init__(self, body):
        self.body = json.dumps(body).encode()
        self.headers = Message()

    def read(self, limit):
        return self.body[:limit]

    def __enter__(self):
        return self

    def __exit__(self, *_args):
        return None


def reader(provider):
    return (
        GitHubForge("github.com", "org", "local", "fixture")
        if provider == "github"
        else GitLabForge("gitlab.example.com", "org/local", "fixture")
    )


def row(provider, path):
    return {"full_name" if provider == "github" else "path_with_namespace": path}


@pytest.mark.parametrize("provider", ["github", "gitlab"])
def test_repository_pages_resume_without_gathering_the_entire_inventory(monkeypatch, provider):
    calls = []
    pages = [[row(provider, "org/one"), row(provider, "org/two")], [row(provider, "other/three")]]

    def urlopen(request, timeout):
        calls.append(request)
        return Response(pages.pop(0))

    monkeypatch.setattr(urllib.request, "urlopen", urlopen)
    client = reader(provider)
    first = client.list_repositories(limit=2)
    assert isinstance(first, Page)
    assert len(calls) == 1
    assert urlsplit(calls[0].full_url).path == (
        "/user/repos" if provider == "github" else "/api/v4/projects"
    )
    assert calls[0].get_header("Authorization" if provider == "github" else "Private-token") == (
        "Bearer fixture" if provider == "github" else "fixture"
    )
    if provider == "gitlab":
        assert parse_qs(urlsplit(calls[0].full_url).query)["membership"] == ["true"]
    assert [item.path for item in first.items] == ["org/one", "org/two"]
    assert first.next_cursor is not None
    assert "fixture" not in json.dumps(asdict(first))
    second = client.list_repositories(cursor=first.next_cursor)
    assert parse_qs(urlsplit(calls[1].full_url).query)["page"] == ["2"]
    assert parse_qs(urlsplit(calls[1].full_url).query)["per_page"] == ["2"]
    assert [item.path for item in second.items] == ["other/three"]
    assert second.next_cursor is None
    assert client.request_counts()["requests"] == 2


@pytest.mark.parametrize("provider", ["github", "gitlab"])
def test_repository_paging_rejects_mismatched_cursor_before_io(monkeypatch, provider):
    calls = []

    def urlopen(request, timeout):
        calls.append(request)
        return Response([row(provider, "org/one")] if len(calls) == 1 else [])

    monkeypatch.setattr(urllib.request, "urlopen", urlopen)
    client = reader(provider)
    first = client.list_repositories(limit=1)
    for kwargs in [
        {"limit": 0},
        {"limit": 101},
        {"limit": True},
        {"cursor": "null"},
        {"cursor": "invalid"},
        {"cursor": first.next_cursor, "limit": 2},
    ]:
        with pytest.raises(ForgeError):
            client.list_repositories(**kwargs)
    other = GitLabForge("another.example.com", "org/repo", "fixture")
    with pytest.raises(ForgeError, match="cursor"):
        other.list_repositories(cursor=first.next_cursor)
    assert len(calls) == 1
    assert client.list_repositories(cursor=first.next_cursor) == Page(())


@pytest.mark.parametrize("provider", ["github", "gitlab"])
@pytest.mark.parametrize(
    "body", [{}, [{}], [{"full_name": "org/../repo", "path_with_namespace": "org/../repo"}]]
)
def test_malformed_repository_page_is_not_an_empty_inventory(monkeypatch, provider, body):
    monkeypatch.setattr(urllib.request, "urlopen", lambda request, timeout: Response(body))
    with pytest.raises(ForgeError):
        reader(provider).list_repositories()


def test_repository_server_failure_propagates(monkeypatch):
    def urlopen(request, timeout):
        raise urllib.error.HTTPError(
            request.full_url, 500, "Unavailable", Message(), io.BytesIO(b"{}")
        )

    monkeypatch.setattr(urllib.request, "urlopen", urlopen)
    with pytest.raises(ForgeError, match="500"):
        reader("gitlab").list_repositories()
