import pytest
from test_operations import run

from repocli import git
from repocli.git_index import (
    deleted_tracked_paths,
    registered_submodules,
    staged_changes,
    status_entries,
)


def test_status_preserves_paths_and_expands_untracked_directories(repo):
    names = [" newline\n中文.txt ", 'quote".txt', "literal -> name", "nested/file.py"]
    for name in names:
        path = repo / name
        path.parent.mkdir(exist_ok=True)
        path.write_text("new")
    (repo / "测试.py").write_text("modified")
    entries = {e.path: e for e in status_entries(repo)}
    assert set(entries) == {*names, "测试.py"}
    assert entries["测试.py"].worktree_status == "M"
    assert all(entries[name].index_status == "?" for name in names)
    assert git.stage(repo, names).ok
    assert {e.path for e in staged_changes(repo)} == set(names)
    assert all(e.new_mode == "100644" for e in staged_changes(repo))


def test_staged_rename_and_deleted_paths(repo):
    name = " renamed\n中文.py "
    run(repo, "mv", "测试.py", name)
    (entry,) = status_entries(repo)
    assert (entry.path, entry.original_path, entry.index_status) == (name, "测试.py", "R")
    changes = {e.path: e for e in staged_changes(repo)}
    assert changes["测试.py"].new_mode == "000000"
    assert changes[name].old_mode == "000000"
    (repo / name).unlink()
    assert deleted_tracked_paths(repo) == [name]


def test_unborn_index_and_query_failures(tmp_path):
    for query in (status_entries, staged_changes, deleted_tracked_paths, registered_submodules):
        with pytest.raises(OSError):
            query(tmp_path)
    run(tmp_path, "init", "-q")
    name = " first\nfile "
    (tmp_path / name).write_text("first")
    assert status_entries(tmp_path)[0].path == name
    assert git.stage(tmp_path, [name]).ok
    (change,) = staged_changes(tmp_path)
    assert (change.path, change.old_mode, change.new_mode) == (name, "000000", "100644")


def test_index_gitlink_modes_and_submodule_registration(repo):
    name = " sub\nmodule "
    head = run(repo, "rev-parse", "HEAD")
    run(repo, "update-index", "--add", "--cacheinfo", f"160000,{head},{name}")
    (change,) = staged_changes(repo)
    assert (change.path, change.old_mode, change.new_mode) == (name, "000000", "160000")
    assert registered_submodules(repo) == []
    run(repo, "config", "--file", ".gitmodules", "submodule.example.path", name)
    assert registered_submodules(repo) == [name]
    assert git.commit(repo, "gitlink").ok
    run(repo, "update-index", "--force-remove", name)
    (change,) = staged_changes(repo)
    assert (change.path, change.old_mode, change.new_mode) == (name, "160000", "000000")
    (repo / ".gitmodules").write_text("[broken")
    with pytest.raises(OSError):
        registered_submodules(repo)
