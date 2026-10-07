import json
from pathlib import Path

import pytest
from test_operations import run

from repocli import git
from repocli import git_state as state

CASES = json.loads((Path(__file__).parents[3] / "conformance/git/scalars.json").read_text())
QUERIES = {
    "branch": state.get_current_branch,
    "head": state.get_head_sha,
    "ref": state.rev_parse,
    "target": state.target_exists,
    "ahead": state.get_ahead_behind,
    "upstream": state.get_upstream_ahead_behind,
    "ancestor": state.is_ancestor,
}


@pytest.mark.parametrize("case", CASES, ids=lambda case: case["name"])
def test_shared_scalar_contract(tmp_path, case):
    root = tmp_path
    if case["mode"] != "nonrepo":
        run(root, "init", "-qb", "main")
        run(root, "config", "user.name", "Fixture")
        run(root, "config", "user.email", "fixture@example.invalid")
        run(root, "config", "commit.gpgsign", "false")
        run(root, "config", "core.hooksPath", "/dev/null")
        if case["mode"] != "unborn":
            run(root, "commit", "--allow-empty", "-qm", "base")
        if case["mode"] == "detached":
            run(root, "checkout", "--detach", "-q")
        if case["mode"] == "diverged":
            run(root, "checkout", "-qb", "other")
            run(root, "commit", "--allow-empty", "-qm", "other")
            run(root, "update-ref", "refs/remotes/origin/main", "HEAD")
            run(root, "checkout", "-q", "main")
            run(root, "commit", "--allow-empty", "-qm", "main")
            run(root, "remote", "add", "origin", "https://example.invalid/repo")
            run(root, "branch", "--set-upstream-to", "origin/main")

    query = QUERIES[case["op"]]
    if case.get("error"):
        with pytest.raises(OSError):
            query(root, *case["args"])
        return
    value = query(root, *case["args"])
    if case["op"] == "head" and value:
        value = "present"
    if isinstance(value, tuple):
        value = list(value)
    assert value == case["expected"]


@pytest.mark.parametrize(
    "op,args",
    [
        ("branch", ()),
        ("head", ()),
        ("ref", ("HEAD",)),
        ("target", ()),
        ("ahead", ()),
        ("upstream", ()),
        ("ancestor", ("HEAD", "HEAD")),
    ],
)
def test_failed_process_never_becomes_a_value(tmp_path, monkeypatch, op, args):
    monkeypatch.setattr(
        git, "git", lambda *a, **kw: git.GitResult(-1, "", "injected timeout", True)
    )
    with pytest.raises(OSError, match="injected timeout"):
        QUERIES[op](tmp_path, *args)


def test_malformed_count_is_an_error(repo, monkeypatch):
    original = git.git

    def run_git(path, *args, **kwargs):
        if args[0] == "rev-list":
            return git.GitResult(0, "1broken\t0", "")
        return original(path, *args, **kwargs)

    run(repo, "update-ref", "refs/remotes/origin/main", "HEAD")
    monkeypatch.setattr(git, "git", run_git)
    with pytest.raises(OSError, match="count divergence"):
        state.get_ahead_behind(repo)
