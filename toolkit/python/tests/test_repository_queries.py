import os

import pytest
from test_operations import run

from repokit import git, git_index, git_state


def test_history_preserves_multiline_messages_and_failed_reads(repo):
    base = git_state.get_head_sha(repo)
    run(repo, "commit", "--allow-empty", "-qm", "subject\n\nbody\nsecond paragraph")
    (commit,) = git_state.commits(repo, base)
    assert commit.subject == "subject" and commit.body == "body\nsecond paragraph\n"
    assert git_state.merge_base(repo, base, "HEAD") == base
    assert git_state.commits(repo, "HEAD") == []
    with pytest.raises(OSError):
        git_state.commits(repo, "missing-ref")
    with pytest.raises(OSError):
        git_state.merge_base(repo, "missing-ref", "HEAD")


def test_exact_checkout_and_literal_branch_names(repo, tmp_path):
    assert git_state.checkout_at(repo).root == str(repo)
    sub = repo / "subdir"
    sub.mkdir()
    assert git_state.checkout_at(sub) is None
    git_state.validate_branch_name(repo, "feature/topic")
    for name in ("", "--help", "bad name", "@{-1}"):
        with pytest.raises(ValueError):
            git_state.validate_branch_name(repo, name)
    (sub / ".git").write_text("gitdir: missing")
    with pytest.raises(OSError):
        git_state.checkout_at(sub)


def test_raw_worktree_comparison_retains_paths_modes_and_partial_staging(repo):
    """@case Comparing a candidate never modifies the index or normalizes source bytes."""
    name = " literal[abc]\nfile "
    file = repo / name
    file.write_text("one\n")
    git.stage(repo, [name])
    index = git_index.index_view(repo)
    assert git_index.working_tree_differences(repo, index) == []
    file.write_text("two\n")
    assert git_index.working_tree_differences(repo, index) == [name]
    assert "+one" in git_index.staged_patch(repo, name)
    file.write_text("one\n")
    file.chmod(0o755)
    assert git_index.working_tree_differences(repo, index) == [name]
    file.unlink()
    os.symlink("target", file)
    assert git_index.working_tree_differences(repo, index) == [name]
    git.stage(repo, [name])
    assert git_index.working_tree_differences(repo, git_index.index_view(repo)) == []
    file.unlink()
    assert git_index.working_tree_differences(repo, index) == [name]


def test_fetch_branches_updates_explicit_tracking_refs(repo, tmp_path):
    remote = tmp_path / "remote.git"
    run(tmp_path, "init", "--bare", str(remote))
    run(repo, "remote", "add", "origin", str(remote))
    run(repo, "push", "origin", "HEAD:refs/heads/main")
    run(repo, "config", "remote.origin.fetch", "+refs/heads/other:refs/remotes/origin/other")
    assert git.fetch_branches(repo, ["main"]).ok
    assert git_state.rev_parse(repo, "refs/remotes/origin/main") == git_state.get_head_sha(repo)
