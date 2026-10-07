import json
import shutil
from pathlib import Path

import pytest
from test_inspect import commit, git, write

from repocli import inspect, owner, snapshot, tree
from repocli.git_state import find_checkout

FIXTURE = json.loads(
    (Path(__file__).resolve().parents[3] / "conformance/tree/entries.json").read_text()
)


def test_tree_versions_roles_and_component_independence(repo):
    write(repo, FIXTURE["files"])
    for path, target in FIXTURE["symlinks"].items():
        (repo / path).symlink_to(target)
    commit(repo)
    oid = git(repo, "rev-parse", "HEAD")
    git(repo, "update-index", "--add", "--cacheinfo", f"160000,{oid},child")
    git(repo, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "link")
    for options in ({}, {"staged": True}, {"head": "HEAD"}):
        report = tree(repo, **options)
        assert report.complete
        assert [d.path for d in report.directories] == FIXTURE["directories"]
        files = {f.path: f for f in report.files}
        assert {f.path: f.role for f in report.files if f.role} == FIXTURE["roles"]
        assert files["child"].kind == "gitlink" and files["child"].oid == oid
        assert files["alias"].kind == "symlink" and files["alias"].role is None
        assert files["testdata/corpus/go.mod"].manifest.ecosystem == "go"
        assert bool(files["go.mod"].oid) == bool(options)
        layout = inspect(repo, **options)
        assert [c.root for c in layout.components] == FIXTURE["components"]
        assert owner(layout, "docs").root == "."
        assert layout.components[0].manifests == ("go.mod",)
    (repo / ".repocli.json").write_text("{")
    assert tree(repo).complete
    with pytest.raises(ValueError):
        inspect(repo)


def test_docs_repository_and_literal_checkout_path(tmp_path):
    root = tmp_path / "repo \n"
    root.mkdir()
    git(root, "init", "-q")
    write(root, {"docs/guide.md": "hello"})
    assert not inspect(root).components
    assert owner(inspect(root), "docs") is None
    assert tree(root).checkout == str(root.resolve())
    assert snapshot(root).checkout == str(root.resolve())
    assert find_checkout(root / "docs") == str(root.resolve())
    assert find_checkout(tmp_path) is None
    (root / ".git/HEAD").write_text("broken")
    with pytest.raises(OSError):
        find_checkout(root)


def test_gitlink_content_not_part_of_parent_snapshot(repo):
    child = repo / "child"
    child.mkdir()
    git(child, "init", "-q")
    write(child, {"README.md": "first"})
    commit(child)
    oid = git(child, "rev-parse", "HEAD")
    git(repo, "update-index", "--add", "--cacheinfo", f"160000,{oid},child")
    before = snapshot(repo)
    (child / "README.md").write_text("dirty")
    (child / "untracked").write_text("dirty")
    assert snapshot(repo).digest == before.digest
    commit(child)
    assert snapshot(repo).digest != before.digest
    shutil.rmtree(child)
    assert snapshot(repo) == before
