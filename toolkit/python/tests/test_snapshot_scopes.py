import importlib
from unittest.mock import patch

import pytest

from repokit import SnapshotScope, component_scopes, inspect, snapshot


def captured(repo, scopes):
    result = snapshot(repo, scopes=scopes)
    assert result.complete
    return result, {item.name: item for item in result.scopes}


def test_component_ownership_and_shared_inputs(repo):
    for name in ("a", "a/child", "b"):
        (repo / name).mkdir(parents=True, exist_ok=True)
        (repo / name / "Makefile").write_text("test:\n\ttrue\n")
        (repo / name / "code.py").write_text("value = 1\n")
    (repo / "Makefile").write_text("test:\n\ttrue\n")
    scopes = component_scopes(inspect(repo))
    first, before = captured(repo, scopes)
    assert before["a"].file_count == 2
    (repo / "b/code.py").write_text("value = 2\n")
    second, after = captured(repo, scopes)
    assert first.digest != second.digest
    assert before["a"] == after["a"] and before["."] == after["."]
    assert before["b"].digest != after["b"].digest
    (repo / "a/child/code.py").write_text("value = 3\n")
    _, nested = captured(repo, scopes)
    assert nested["a"] == after["a"] and nested["a/child"] != after["a/child"]
    # No scopes preserves the whole-repository digest shared with Go/TypeScript.
    assert snapshot(repo).digest == snapshot(repo, scopes=scopes).digest


def test_scope_membership_add_delete_rename_and_definition(repo):
    scopes = {"inputs": SnapshotScope(("new",))}
    _, empty = captured(repo, scopes)
    (repo / "new").mkdir()
    (repo / "new/a").write_text("same bytes")
    _, added = captured(repo, scopes)
    (repo / "new/a").rename(repo / "new/b")
    _, renamed = captured(repo, scopes)
    (repo / "new/b").unlink()
    (repo / "new").rmdir()
    _, deleted = captured(repo, scopes)
    assert empty == deleted
    assert len({empty["inputs"].digest, added["inputs"].digest, renamed["inputs"].digest}) == 3
    _, different = captured(repo, {"inputs": SnapshotScope(("other",))})
    assert empty["inputs"].digest != different["inputs"].digest


def test_scope_large_files_and_symlink_boundaries(repo):
    (repo / "a").mkdir()
    (repo / "a/large").write_bytes(b"a" * (3 << 20))
    (repo / "a/link").symlink_to("../测试.py")
    scopes = {"a": SnapshotScope(("a",)), "both": SnapshotScope(("a", "测试.py"))}
    _, before = captured(repo, scopes)
    assert not before["a"].complete and before["both"].complete
    (repo / "a/large").write_bytes(b"b" * (3 << 20))
    _, after = captured(repo, scopes)
    assert before["both"].digest != after["both"].digest


def test_scope_projection_uses_one_capture_pair_and_detects_concurrent_edits(repo):
    module = importlib.import_module("repokit.snapshot")
    original = module._capture
    calls = []

    def capture(root, budget, scopes):
        result = original(root, budget, scopes)
        calls.append(result)
        if len(calls) == 1:
            (repo / "测试.py").write_text("concurrent edit")
        return result

    with patch.object(module, "_capture", side_effect=capture):
        observed = snapshot(repo, scopes={str(i): SnapshotScope((".",)) for i in range(12)})
    assert len(calls) == 2
    assert not observed.complete and all(not item.complete for item in observed.scopes)


@pytest.mark.parametrize("path", ["", "/tmp", "../outside", "a/../b", "a/", "./a"])
def test_scope_rejects_ambiguous_and_outside_paths(path):
    with pytest.raises(ValueError):
        SnapshotScope((path,))


def test_scope_canonicalizes_sets():
    assert SnapshotScope(("b", "a", "a")) == SnapshotScope(("a", "b"))


def test_explicit_ignored_input_is_not_a_complete_scope(repo):
    (repo / ".gitignore").write_text("secret.cfg\n")
    (repo / "secret.cfg").write_text("not captured")
    result = snapshot(repo, scopes={"config": SnapshotScope(("secret.cfg",))})
    assert result.complete and not result.scopes[0].complete
