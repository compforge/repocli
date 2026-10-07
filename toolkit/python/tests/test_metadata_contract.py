import json
from pathlib import Path

import pytest
from test_operations import run

from repocli.git_state import git_path, rebase_in_progress

CONTRACT = json.loads((Path(__file__).parents[3] / "conformance/git/metadata.json").read_text())


@pytest.mark.parametrize("case", CONTRACT["layouts"], ids=lambda case: case["name"])
def test_metadata_contract(tmp_path, case):
    root = tmp_path / " 仓库\n "
    linked = tmp_path / "linked"
    metadata = tmp_path / " metadata "
    root.mkdir()
    run(root, "init", "-qb", "main")
    run(
        root,
        "-c",
        "user.name=Fixture",
        "-c",
        "user.email=fixture@example.invalid",
        "-c",
        "core.hooksPath=/dev/null",
        "-c",
        "commit.gpgsign=false",
        "commit",
        "--allow-empty",
        "-qm",
        "initial",
    )
    common = root / ".git"
    if case["mode"] == "separate":
        run(root, "init", "--separate-git-dir", str(metadata))
        common = metadata
    elif case["mode"] == "bare":
        run(root, "clone", "--bare", str(root), str(metadata))
        common = metadata
    run(
        metadata if case["mode"] == "bare" else root,
        "worktree",
        "add",
        "-qb",
        "topic",
        str(linked),
        "HEAD",
    )
    view = linked if case["view"] == "linked" else root
    local = common / "worktrees/linked" if case["view"] == "linked" else common
    for entry in CONTRACT["paths"]:
        expected = (common if entry["shared"] else local) / entry["name"]
        assert git_path(view, entry["name"]) == expected
    assert not rebase_in_progress(view)


def test_metadata_failure_is_not_absence(tmp_path):
    with pytest.raises(OSError):
        git_path(tmp_path, "index")
    with pytest.raises(OSError):
        rebase_in_progress(tmp_path)


def test_rebase_state_unreadable(repo, monkeypatch):
    state = git_path(repo, "rebase-merge")
    original = Path.stat

    def stat(path, *args, **kwargs):
        if path == state:
            raise PermissionError("metadata unreadable")
        return original(path, *args, **kwargs)

    monkeypatch.setattr(Path, "stat", stat)
    with pytest.raises(PermissionError, match="metadata unreadable"):
        rebase_in_progress(repo)
