from repocli.forge import GitHubForge, GitLabForge
from repocli.forge._rest import RestClient
from repocli.forge.model import parse_pr_number


def test_rest_get_all_pages_until_short_batch():
    """List endpoints must not silently truncate at the first page: review→label uses the
    forge as its durable source of truth, and two review rounds can already exceed 50 comments."""
    c = RestClient.__new__(RestClient)
    calls = []

    def get(path, **params):
        calls.append((path, params))
        return list(range(100)) if params["page"] == 1 else [100]

    c.get = get
    assert len(c.get_all("comments")) == 101
    assert calls == [
        ("comments", {"page": 1, "per_page": 100}),
        ("comments", {"page": 2, "per_page": 100}),
    ]


def test_parse_pr_number():
    assert parse_pr_number("https://github.com/o/r/pull/12") == 12
    assert parse_pr_number("https://gitlab.com/g/p/-/merge_requests/7") == 7
    assert parse_pr_number("#5") == 5 and parse_pr_number("!9") == 9 and parse_pr_number("42") == 42
    assert parse_pr_number("nope") is None


def test_forge_pr_mapping():
    """Each adapter normalizes its native JSON → the neutral PullRequest: iid/number,
    GitHub's open/closed + merged flag → open|merged|closed, head/base → source/target.
    No provider on the PR — that's repo-level."""
    gl = GitLabForge.__new__(GitLabForge)  # bypass __init__ (no HTTP needed for mapping)
    pr = gl._to_pr(
        {
            "iid": 7,
            "state": "opened",
            "source_branch": "f",
            "target_branch": "m",
            "web_url": "u",
            "sha": "abc",
        }
    )
    assert (pr.number, pr.state, pr.source_branch, pr.sha) == (7, "open", "f", "abc")
    assert not hasattr(pr, "provider")
    assert gl._to_pr({"iid": 8, "state": "merged"}).state == "merged"
    assert gl._to_pr({"iid": 9, "state": "locked"}).state == "closed"

    gh = GitHubForge.__new__(GitHubForge)
    gh.owner, gh.name = "o", "r"
    pr = gh._to_pr(
        {
            "number": 12,
            "state": "open",
            "head": {"ref": "f", "sha": "abc"},
            "base": {"ref": "main"},
            "html_url": "u",
        }
    )
    assert (pr.number, pr.state, pr.source_branch, pr.target_branch) == (12, "open", "f", "main")
    # closed + merged_at → merged; closed without merge → closed
    assert gh._to_pr({"number": 13, "state": "closed", "merged_at": "2026-01-01"}).state == "merged"
    assert gh._to_pr({"number": 14, "state": "closed"}).state == "closed"
    assert gh._to_pr({"number": 15, "state": "closed", "merged": True}).state == "merged"


def test_forge_merge_readiness_mapping():
    """GitLab detailed_merge_status → neutral MergeReadiness, with a has_conflicts fallback and
    UNKNOWN for the async 'checking' window (UNKNOWN must never collapse to READY/CONFLICT).
    GitHub inherits the safe UNKNOWN default since it's not implemented yet."""
    from repocli.forge.model import MergeReadiness

    r = GitLabForge._readiness
    assert r({"detailed_merge_status": "mergeable"}) is MergeReadiness.READY
    assert r({"detailed_merge_status": "conflict"}) is MergeReadiness.CONFLICT
    assert (
        r({"detailed_merge_status": "discussions_not_resolved"})
        is MergeReadiness.DISCUSSIONS_UNRESOLVED
    )
    assert r({"detailed_merge_status": "ci_still_running"}) is MergeReadiness.CI_BLOCKED
    assert (
        r({"detailed_merge_status": "checking"}) is MergeReadiness.UNKNOWN
    )  # async window, not a verdict
    assert r({}) is MergeReadiness.UNKNOWN
    assert r({"has_conflicts": True}) is MergeReadiness.CONFLICT  # fallback when no detailed status
    # GitHub adapter hasn't implemented it → inherits the safe UNKNOWN default (no HTTP)
    assert GitHubForge.__new__(GitHubForge).merge_readiness(0) is MergeReadiness.UNKNOWN


def test_forge_release_endpoints():
    """create_release / latest_release hit the right endpoint with the provider's field names
    and map back to the neutral Release: github POST/GET releases (`target_commitish`/`body`,
    `/releases/latest` with 404→None), gitlab POST/GET releases (`ref`/`description`, list→first).
    """
    from repocli.forge.github import GitHubForge
    from repocli.forge.gitlab import GitLabForge
    from repocli.forge.model import ForgeNotFound

    class _Cap:
        def __init__(self, get_resp=None, get_exc=None):
            self.calls = []
            self._get, self._get_exc = get_resp, get_exc
            self.gets = []

        def get(self, path, **kw):
            self.gets.append((path, kw))
            if self._get_exc:
                raise self._get_exc
            return self._get

        def post(self, path, body):
            self.calls.append((path, body))
            return {
                "tag_name": body.get("tag_name"),
                "name": body.get("name"),
                "html_url": f"gh/{body.get('tag_name')}",
                "target_commitish": body.get("target_commitish"),
                "_links": {"self": f"gl/{body.get('tag_name')}"},
                "commit": {"id": "deadbeef"},
                "published_at": "2026-07-05",
            }

    gh = GitHubForge("api.github.com", "o", "r", "t")
    gh.c = _Cap()
    rel = gh.create_release(tag="v1.8.0", target="main", notes="hi")
    assert gh.c.calls[0] == (
        "releases",
        {"tag_name": "v1.8.0", "target_commitish": "main", "name": "v1.8.0", "body": "hi"},
    )
    assert (rel.tag, rel.target, rel.web_url) == ("v1.8.0", "main", "gh/v1.8.0")

    gl = GitLabForge("h", "o/r", "t")
    gl.c = _Cap()
    rel = gl.create_release(tag="v1.8.0", target="release", name="R", notes="hi")
    assert gl.c.calls[0] == (
        "releases",
        {"tag_name": "v1.8.0", "ref": "release", "name": "R", "description": "hi"},
    )
    assert (rel.tag, rel.name, rel.target, rel.web_url) == ("v1.8.0", "R", "deadbeef", "gl/v1.8.0")

    # latest: github 404 → None (first release); a hit → mapped
    gh2 = GitHubForge("api.github.com", "o", "r", "t")
    gh2.c = _Cap(get_exc=ForgeNotFound("404"))
    assert gh2.latest_release() is None and gh2.c.gets[0][0] == "releases/latest"
    gh3 = GitHubForge("api.github.com", "o", "r", "t")
    gh3.c = _Cap(get_resp={"tag_name": "v1.7.2", "html_url": "u"})
    assert gh3.latest_release().tag == "v1.7.2"
    # gitlab: list newest-first → first; empty list → None
    gl2 = GitLabForge("h", "o/r", "t")
    gl2.c = _Cap(get_resp=[{"tag_name": "v1.7.2"}])
    assert gl2.latest_release().tag == "v1.7.2" and gl2.c.gets[0] == ("releases", {"per_page": 1})
    gl3 = GitLabForge("h", "o/r", "t")
    gl3.c = _Cap(get_resp=[])
    assert gl3.latest_release() is None


def test_forge_comment_endpoint():
    """Summary stays a plain note; a GitLab fallback finding can use an unanchored discussion."""
    from repocli.forge.github import GitHubForge
    from repocli.forge.gitlab import GitLabForge

    class _Cap:
        def __init__(self):
            self.calls = []

        def post(self, path, body):
            self.calls.append((path, body))
            return {"id": 1}

    gl = GitLabForge("h", "o/r", "t")
    gl.c = _Cap()
    gl.comment(7, "hi")
    assert gl.c.calls == [("merge_requests/7/notes", {"body": "hi"})]
    gl.comment(7, "finding", replyable=True)
    assert gl.c.calls[-1] == ("merge_requests/7/discussions", {"body": "finding"})
    gh = GitHubForge("api.github.com", "o", "r", "t")
    gh.c = _Cap()
    gh.comment(7, "hi")
    assert gh.c.calls == [("issues/7/comments", {"body": "hi"})]


def test_forge_replyable_comment_endpoint():
    """comment(replyable=True) 发行可回复 comment：GitLab → positioned/unanchored discussion
    （diff_refs memo，一轮 N 条只 GET 一次）；github → pulls/{n}/comments 带 head-sha commit_id
    （同样 memo）。不支持的 shape 明确 raise，让调用方决定是否回落。"""
    from repocli.forge.github import GitHubForge
    from repocli.forge.gitlab import GitLabForge
    from repocli.forge.model import ForgeError

    class _Cap:
        def __init__(self, get_resp):
            self.calls = []
            self._get = get_resp
            self.gets = 0

        def get(self, path, **kw):
            self.gets += 1
            return self._get

        def post(self, path, body):
            self.calls.append((path, body))
            return {"id": 1}

    gl = GitLabForge("h", "o/r", "t")
    gl.c = _Cap({"diff_refs": {"base_sha": "b", "start_sha": "s", "head_sha": "h"}})
    gl.comment(7, "hi", replyable=True, path="a.py", line=5)
    gl.comment(7, "yo", replyable=True, path="b.py", line=9)
    assert gl.c.gets == 1  # diff_refs memoized
    path, body = gl.c.calls[0]
    assert path == "merge_requests/7/discussions" and body["body"] == "hi"
    assert body["position"]["new_path"] == "a.py" and body["position"]["new_line"] == 5
    assert body["position"]["head_sha"] == "h"

    gh = GitHubForge("api.github.com", "o", "r", "t")
    gh.c = _Cap({"head": {"sha": "abc"}})
    gh.comment(7, "hi", replyable=True, path="a.py", line=5)
    gh.comment(7, "yo", replyable=True, path="b.py", line=9)
    assert gh.c.gets == 1  # head sha memoized
    assert gh.c.calls[0] == (
        "pulls/7/comments",
        {"body": "hi", "commit_id": "abc", "path": "a.py", "line": 5, "side": "RIGHT"},
    )

    # line=None → 文件级锚点：同一端点少一个字段（gitlab position_type=file / github
    # subject_type=file）。github 侧 line/side 必须整个省掉,给 null 会 422。
    gl3 = GitLabForge("h", "o/r", "t")
    gl3.c = _Cap({"diff_refs": {"base_sha": "b", "start_sha": "s", "head_sha": "h"}})
    gl3.comment(7, "hi", replyable=True, path="a.py")
    pos = gl3.c.calls[0][1]["position"]
    assert pos["position_type"] == "file" and pos["new_path"] == "a.py"
    assert "new_line" not in pos

    gh3 = GitHubForge("api.github.com", "o", "r", "t")
    gh3.c = _Cap({"head": {"sha": "abc"}})
    gh3.comment(7, "hi", replyable=True, path="a.py")
    assert gh3.c.calls[0] == (
        "pulls/7/comments",
        {"body": "hi", "commit_id": "abc", "path": "a.py", "subject_type": "file"},
    )

    try:
        gl.comment(7, "x", path="a.py")
        raise AssertionError("standalone comments must reject a diff anchor")
    except ForgeError:
        pass

    try:
        gh.comment(7, "unanchored", replyable=True)
        raise AssertionError("GitHub should reject an unanchored replyable comment")
    except ForgeError:
        pass

    gl2 = GitLabForge("h", "o/r", "t")  # 缺 sha 的 diff_refs → 提前明确报错,
    gl2.c = _Cap({"diff_refs": {"head_sha": "h"}})  # 不让 None 漏进 position 变成盲 400
    try:
        gl2.comment(7, "hi", replyable=True, path="a.py", line=5)
        raise AssertionError("partial diff_refs should raise")
    except ForgeError as e:
        assert "base_sha" in str(e) and "start_sha" in str(e)


def test_forge_comments_union_both_surfaces():
    """comments() 把两个 provider 表面收成顶层 Comment + nested replies。

    GitHub 拆成 issues/{n}+pulls/{n} 两次 GET；GitLab 一次 discussions 覆盖两者。
    调用方不再按 thread id 自己 join，system note 也不会穿透 adapter。
    """
    from repocli.forge.github import GitHubForge
    from repocli.forge.gitlab import GitLabForge
    from repocli.forge.model import CommentResolution

    class _Cap:
        def __init__(self, by_path):
            self._by = by_path
            self.gets = []

        def get_all(self, path, **kw):
            self.gets.append(path)
            return self._by.get(path, [])

    class _Graph:
        def __init__(self):
            self.calls = []

        def post(self, path, body):
            self.calls.append((path, body))
            return {
                "data": {
                    "repository": {
                        "pullRequest": {
                            "reviewThreads": {
                                "nodes": [
                                    {
                                        "id": "PRRT_1",
                                        "isResolved": False,
                                        "comments": {"nodes": [{"fullDatabaseId": "20"}]},
                                    }
                                ],
                                "pageInfo": {"hasNextPage": False, "endCursor": None},
                            }
                        }
                    }
                }
            }

    gh = GitHubForge("api.github.com", "o", "r", "t")
    gh.c = _Cap(
        {
            "issues/7/comments": [
                {
                    "id": 1,
                    "user": {"login": "amy"},
                    "body": "summary",
                    "created_at": "2026-07-01T00:00:00Z",
                },
            ],
            "pulls/7/comments": [
                {
                    "id": 20,
                    "user": {"login": "bot"},
                    "body": "finding ccr:fp=abc",
                    "path": "a.py",
                    "line": 5,
                    "created_at": "2026-07-02T00:00:00Z",
                },
                {
                    "id": 21,
                    "user": {"login": "amy"},
                    "body": "ccr:label=wrong",
                    "path": "a.py",
                    "in_reply_to_id": 20,
                    "line": None,
                    "original_line": 5,
                    "created_at": "2026-07-03T00:00:00Z",
                },
            ],
        }
    )
    gh.g = _Graph()
    cs = gh.comments(7)
    assert sorted(gh.c.gets) == ["issues/7/comments", "pulls/7/comments"]  # 两个面都读了
    assert [c.body for c in cs] == ["summary", "finding ccr:fp=abc"]
    assert (cs[0].id, cs[0].replyable, cs[0].path) == ("1", False, "")
    assert (cs[1].id, cs[1].reply_ref, cs[1].resolve_ref) == ("20", "20", "PRRT_1")
    assert cs[1].resolution is CommentResolution.UNRESOLVED
    assert [reply.body for reply in cs[1].replies] == ["ccr:label=wrong"]
    assert cs[1].replies[0].line == 5  # null line 回落 original_line（写时的位置）
    _, graphql_body = gh.g.calls[0]
    assert graphql_body["variables"] == {
        "owner": "o",
        "name": "r",
        "number": 7,
        "cursor": None,
    }

    gl = GitLabForge("h", "o/r", "t")
    gl.c = _Cap(
        {
            "merge_requests/7/discussions": [
                {
                    "id": "d1",
                    "individual_note": True,
                    "notes": [{"id": 1, "author": {"username": "amy"}, "body": "summary"}],
                },
                {
                    "id": "d2",
                    "individual_note": False,
                    "notes": [
                        {
                            "id": 20,
                            "author": {"username": "bot"},
                            "body": "finding ccr:fp=abc",
                            "position": {"new_path": "a.py", "new_line": 5},
                            "resolvable": True,
                            "resolved": False,
                        },
                        {"id": 21, "author": {"username": "amy"}, "body": "ccr:label=wrong"},
                        {"id": 99, "system": True, "body": "changed the description"},
                    ],
                },
                {
                    "id": "d3",
                    "individual_note": False,
                    "notes": [
                        {
                            "id": 30,
                            "author": {"username": "bot"},
                            "body": "fallback finding ccr:fp=def",
                        },
                    ],
                },
            ]
        }
    )
    cs = gl.comments(7)
    assert gl.c.gets == ["merge_requests/7/discussions"]  # 一次就够，不用第二个面
    assert [c.body for c in cs] == [
        "summary",
        "finding ccr:fp=abc",
        "fallback finding ccr:fp=def",
    ]
    assert not cs[0].replyable  # individual_note 是普通 note 的包装，回不进去
    assert (cs[1].id, cs[1].reply_ref, cs[1].resolve_ref) == ("20", "d2", "d2")
    assert [(reply.id, reply.body) for reply in cs[1].replies] == [
        ("21", "ccr:label=wrong"),
    ]
    assert (cs[1].path, cs[1].line) == ("a.py", 5)
    assert cs[1].resolution is CommentResolution.UNRESOLVED
    assert cs[0].resolution is CommentResolution.UNSUPPORTED
    # Older/self-managed GitLab can omit resolvability fields. The discussion id still reaches
    # the authoritative resolve endpoint; only its cached state remains unknown.
    assert (cs[2].reply_ref, cs[2].resolve_ref) == ("d3", "d3")
    assert cs[2].resolution is CommentResolution.UNSUPPORTED


def test_github_review_threads_paginate_and_report_resolution():
    """GitHub's resolvable handle/state live on paginated GraphQL review threads."""
    from repocli.forge.github import GitHubForge

    def response(nodes, *, next_cursor=None):
        return {
            "data": {
                "repository": {
                    "pullRequest": {
                        "reviewThreads": {
                            "nodes": nodes,
                            "pageInfo": {
                                "hasNextPage": next_cursor is not None,
                                "endCursor": next_cursor,
                            },
                        }
                    }
                }
            }
        }

    class _Graph:
        def __init__(self):
            self.calls = []
            self.responses = [
                response(
                    [
                        {
                            "id": "PRRT_1",
                            "isResolved": False,
                            "comments": {"nodes": [{"fullDatabaseId": "20"}]},
                        }
                    ],
                    next_cursor="page-2",
                ),
                response(
                    [
                        {
                            "id": "PRRT_2",
                            "isResolved": True,
                            "comments": {"nodes": [{"fullDatabaseId": "30"}]},
                        }
                    ]
                ),
            ]

        def post(self, path, body):
            self.calls.append((path, body))
            return self.responses.pop(0)

    gh = GitHubForge("github.com", "o", "r", "t")
    gh.g = _Graph()
    assert gh._review_threads(7) == {
        "20": ("PRRT_1", False),
        "30": ("PRRT_2", True),
    }
    assert [body["variables"]["cursor"] for _, body in gh.g.calls] == [None, "page-2"]


def test_forge_reply_endpoint():
    """reply() 消费 adapter 产生的 opaque reply_ref。

    GitLab 写 discussion notes，GitHub 写 root review comment replies；普通 comment 没有
    reply_ref 时明确失败，不静默发布游离回复。
    """
    from repocli.forge.github import GitHubForge
    from repocli.forge.gitlab import GitLabForge
    from repocli.forge.model import Comment, Forge, ForgeError

    class _Cap:
        def __init__(self):
            self.calls = []

        def post(self, path, body):
            self.calls.append((path, body))
            return {"id": 1}

    gl = GitLabForge("h", "o/r", "t")
    gl.c = _Cap()
    gl.reply(7, Comment(id="20", reply_ref="d2"), "ccr:label=important")
    assert gl.c.calls == [
        ("merge_requests/7/discussions/d2/notes", {"body": "ccr:label=important"})
    ]

    gh = GitHubForge("api.github.com", "o", "r", "t")
    gh.c = _Cap()
    gh.reply(7, Comment(id="20", reply_ref="20"), "ccr:label=important")
    assert gh.c.calls == [("pulls/7/comments/20/replies", {"body": "ccr:label=important"})]

    for f in (gl, gh):  # 普通评论无线程 → 明确报错,不静默游离
        try:
            f.reply(7, Comment(id="1"), "x")
            raise AssertionError("reply to a non-threadable comment should raise")
        except ForgeError:
            pass

    try:  # 端口默认：不支持 → raise
        Forge.reply(gl, 7, Comment(id="1", reply_ref="d2"), "x")
        raise AssertionError("default reply should raise")
    except ForgeError:
        pass


def test_forge_resolve_comment_endpoint():
    """Each provider resolves the interaction behind a comment via opaque resolve_ref."""
    from repocli.forge.github import GitHubForge
    from repocli.forge.gitlab import GitLabForge
    from repocli.forge.model import Comment, ForgeError

    class _Cap:
        def __init__(self):
            self.calls = []

        def put(self, path, body):
            self.calls.append((path, body))
            return {}

    gl = GitLabForge("h", "o/r", "t")
    gl.c = _Cap()
    gl.resolve_comment(7, Comment(id="20", resolve_ref="d2"))
    assert gl.c.calls == [("merge_requests/7/discussions/d2", {"resolved": True})]

    class _Graph:
        def __init__(self):
            self.calls = []

        def post(self, path, body):
            self.calls.append((path, body))
            return {
                "data": {
                    "resolveReviewThread": {
                        "thread": {"id": "PRRT_1", "isResolved": True},
                    }
                }
            }

    gh = GitHubForge("github.com", "o", "r", "t")
    gh.g = _Graph()
    gh.resolve_comment(7, Comment(id="20", resolve_ref="PRRT_1"))
    path, body = gh.g.calls[0]
    assert path == "" and body["variables"] == {"thread": "PRRT_1"}
    assert "resolveReviewThread" in body["query"]

    try:
        gl.resolve_comment(7, Comment(id="1"))
        raise AssertionError("non-resolvable comment should raise")
    except ForgeError:
        pass

    try:
        gh.resolve_comment(7, Comment(id="1"))
        raise AssertionError("non-resolvable GitHub comment should raise")
    except ForgeError:
        pass


def test_github_graphql_surfaces_api_errors():
    """GraphQL can return HTTP 200 with an errors body; do not treat it as success."""
    from repocli.forge.github import GitHubForge
    from repocli.forge.model import ForgeError

    class _Graph:
        def post(self, path, body):
            return {"data": None, "errors": [{"message": "Resource not accessible"}]}

    gh = GitHubForge("github.com", "o", "r", "t")
    gh.g = _Graph()
    try:
        gh._graphql("query { viewer { login } }", {})
        raise AssertionError("GraphQL errors should raise")
    except ForgeError as error:
        assert "Resource not accessible" in str(error)


def test_forge_default_branch():
    """default_branch() 读 repo 根对象的 default_branch（gitlab GET /projects/{id}、
    github GET /repos/{o}/{n}，路径为 ""）；缺字段 → ""。"""
    from repocli.forge.github import GitHubForge
    from repocli.forge.gitlab import GitLabForge

    class _C:
        def __init__(self, d):
            self.d, self.paths = d, []

        def get(self, path):
            self.paths.append(path)
            return self.d

    gl = GitLabForge("h", "o/r", "t")
    gl.c = _C({"default_branch": "release"})
    assert gl.default_branch() == "release" and gl.c.paths == [""]  # repo root
    gh = GitHubForge("api.github.com", "o", "r", "t")
    gh.c = _C({"default_branch": "main"})
    assert gh.default_branch() == "main"
    gl2 = GitLabForge("h", "o/r", "t")
    gl2.c = _C({})
    assert gl2.default_branch() == ""  # missing field → empty


def test_merge_requires_reviewed_tip_and_maps_endpoints():
    from unittest.mock import Mock

    import pytest

    for cls, number_field, endpoint in (
        (GitHubForge, "number", "pulls"),
        (GitLabForge, "iid", "merge_requests"),
    ):
        client = cls.__new__(cls)
        client.c = Mock()
        client.c.put.return_value = {number_field: 7, "merged": True, "state": "merged"}
        client.c.get.return_value = {number_field: 7, "merged": True, "state": "closed"}
        with pytest.raises(ValueError):
            client.merge(7, expected_sha="")
        assert client.merge(7, expected_sha="reviewed").state == "merged"
        client.c.put.assert_called_once_with(f"{endpoint}/7/merge", {"sha": "reviewed"})


def test_invalid_later_page_does_not_return_partial_inventory():
    from unittest.mock import patch

    import pytest

    from repocli.forge.model import ForgeError

    client = RestClient("https://example.invalid", {})
    with patch.object(client, "get", side_effect=[list(range(100)), {"unexpected": "object"}]):
        with pytest.raises(ForgeError, match="page 2"):
            client.get_all("pulls")
