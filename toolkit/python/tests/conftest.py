import pytest
from test_operations import run

from repocli import git


@pytest.fixture
def repo(tmp_path):
    run(tmp_path, "init", "-q")
    run(tmp_path, "config", "user.name", "Fixture")
    run(tmp_path, "config", "user.email", "fixture@example.invalid")
    run(tmp_path, "config", "core.hooksPath", "/dev/null")
    (tmp_path / "测试.py").write_text("print(1)\n")
    assert git.stage(tmp_path, ["测试.py"]).ok
    assert git.commit(tmp_path, "initial").ok
    return tmp_path
