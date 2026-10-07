import json
from pathlib import Path

import pytest
from test_operations import run

from repocli.git_state import checkout_info, list_checkouts

CASES = json.loads((Path(__file__).parents[3] / "conformance/git/checkouts.json").read_text())


@pytest.mark.parametrize("case", CASES, ids=lambda case: case["name"])
def test_shared_checkout_contract(tmp_path, case):
    paths = {
        "root": tmp_path / "repo",
        "linked": tmp_path / " linked\n工作区 ",
        "metadata": tmp_path / "metadata",
        "missing": tmp_path / "missing",
    }
    root = paths["root"]
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
    if case["mode"] == "separate":
        run(root, "init", "--separate-git-dir", str(paths["metadata"]))
    elif case["mode"] == "bare":
        run(root, "clone", "--bare", str(root), str(paths["metadata"]))
    control = paths["metadata"] if case["mode"] == "bare" else root
    run(control, "worktree", "add", "-qb", "topic", str(paths["linked"]), "HEAD")
    view = paths[case["view"]]
    if case.get("error"):
        for query in (checkout_info, list_checkouts):
            with pytest.raises(OSError):
                query(view)
        return

    def label(path):
        return next(
            (name for name, location in paths.items() if str(location.resolve()) == path), path
        )

    info = checkout_info(view)
    actual = {
        "root": label(info.root),
        "mainRoot": label(info.main_root),
        "linked": info.linked,
        "entries": [
            {"path": label(e.path), "branch": e.branch, "primary": e.primary}
            for e in list_checkouts(view)
        ],
    }
    assert actual == case["expected"]
