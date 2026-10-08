import json
import os
import subprocess
import sys
import time
from concurrent.futures import ThreadPoolExecutor
from threading import Event

import pytest

from repocli import inspect_dependencies, operation, prepare_dependencies


@pytest.fixture
def repo(tmp_path):
    root = tmp_path / "checkout"
    root.mkdir()
    subprocess.run(["git", "init", "-q", str(root)], check=True)
    (root / ".gitignore").write_text("node_modules/\n.venv/\n")
    return root


def node(root, manager="bun", extra=None):
    (root / "package.json").write_text(
        json.dumps({"name": "fixture", "packageManager": manager + "@1.0.0", **(extra or {})})
    )
    lock = {
        "bun": "bun.lock",
        "pnpm": "pnpm-lock.yaml",
        "npm": "package-lock.json",
        "yarn": "yarn.lock",
    }[manager]
    (root / lock).write_text("locked fixture\n")


def installer(tmp_path, monkeypatch, name="bun", body="Path('node_modules').mkdir(exist_ok=True)"):
    binary = tmp_path / "bin"
    binary.mkdir(exist_ok=True)
    script = binary / name
    script.write_text(
        f"#!{sys.executable}\nfrom pathlib import Path\nimport json, os, sys, time\n"
        + "with Path('invocations').open('a') as out:\n"
        + "    print(json.dumps({'cwd': os.getcwd(), 'args': sys.argv[1:], "
        + "'venv': os.getenv('VIRTUAL_ENV'), "
        + "'uv_env': os.getenv('UV_PROJECT_ENVIRONMENT')}), file=out)\n"
        + body
        + "\n"
    )
    script.chmod(0o755)
    monkeypatch.setenv("PATH", str(binary) + os.pathsep + os.environ["PATH"])


def test_read_only_observation_does_not_install_or_borrow_parent(repo):
    node(repo)
    (repo.parent / "node_modules").mkdir()
    (observed,) = inspect_dependencies(repo)
    assert observed.status == "missing"
    assert observed.command == ("bun", "install", "--frozen-lockfile")
    assert not (repo / "node_modules").exists()
    (repo / "node_modules").mkdir()
    assert inspect_dependencies(repo)[0].status == "present"
    assert not (repo / "node_modules" / ".repocli-dependencies.json").exists()


def test_workspace_members_share_one_preparation_and_member_changes_invalidate(
    repo, tmp_path, monkeypatch
):
    node(repo, extra={"workspaces": ["packages/*", "!packages/excluded"]})
    members = [repo / "packages" / name for name in ("a", "b", "excluded")]
    for member in members:
        member.mkdir(parents=True)
        (member / "package.json").write_text('{"name":"member"}')
    installer(
        tmp_path, monkeypatch, body="time.sleep(0.05); Path('node_modules').mkdir(exist_ok=True)"
    )
    observed = [inspect_dependencies(member)[0] for member in members[:2]]
    assert all(item.root == repo for item in observed)
    assert inspect_dependencies(members[2])[0].root == members[2]
    with ThreadPoolExecutor(2) as pool:
        results = list(pool.map(prepare_dependencies, observed))
    assert all(result.status == "ready" for result in results)
    assert sum(result.executed for result in results) == 1
    assert len((repo / "invocations").read_text().splitlines()) == 1
    (members[0] / "package.json").write_text('{"name":"changed"}')
    assert inspect_dependencies(members[1])[0].status == "stale"
    assert prepare_dependencies(observed[0]).status == "failed"


def test_pnpm_yaml_and_uv_workspace_installation_roots(repo, tmp_path, monkeypatch):
    node(repo, "pnpm")
    (repo / "pnpm-workspace.yaml").write_text("packages:\n  - 'packages/*'\n")
    member = repo / "packages" / "child"
    member.mkdir(parents=True)
    (member / "package.json").write_text('{"name":"child"}')
    assert inspect_dependencies(member)[0].root == repo
    (repo / "pyproject.toml").write_text(
        '[project]\nname="root"\nversion="1"\n[tool.uv.workspace]\nmembers=["python/*"]\n'
    )
    (repo / "uv.lock").write_text("version = 1\n")
    child = repo / "python" / "child"
    child.mkdir(parents=True)
    (child / "pyproject.toml").write_text('[project]\nname="child"\nversion="1"\n')
    installer(tmp_path, monkeypatch, "uv", "Path('.venv').mkdir(exist_ok=True)")
    monkeypatch.setenv("VIRTUAL_ENV", "/other/checkout/.venv")
    monkeypatch.setenv("UV_PROJECT_ENVIRONMENT", "/other/checkout/.venv")
    (environment,) = inspect_dependencies(child)
    assert environment.root == repo
    assert environment.command == ("uv", "sync", "--locked", "--all-packages")
    result = prepare_dependencies(environment)
    assert result.status == "ready"
    call = json.loads((repo / "invocations").read_text())
    assert call["venv"] is None and call["uv_env"] is None


def test_unsupported_preparation_is_distinct_from_existing_local_dependencies(repo):
    (repo / "package.json").write_text('{"name":"unlocked"}')
    (missing,) = inspect_dependencies(repo)
    assert missing.status == "unsupported" and not missing.command
    assert prepare_dependencies(missing).status == "unsupported"
    (repo / "node_modules").mkdir()
    assert inspect_dependencies(repo)[0].status == "present"


@pytest.mark.parametrize(
    "body,reason",
    [
        (
            "sys.stderr.write('registry unavailable'); sys.exit(7)",
            "Locked dependency installation failed",
        ),
        ("pass", "Installer did not materialize"),
        (
            "Path('node_modules').mkdir(); Path('bun.lock').write_text('changed')",
            "Dependency inputs changed",
        ),
    ],
)
def test_installation_failure_never_creates_ready_receipt(
    repo, tmp_path, monkeypatch, body, reason
):
    node(repo)
    installer(tmp_path, monkeypatch, body=body)
    result = prepare_dependencies(inspect_dependencies(repo)[0])
    assert result.status == "failed" and reason in result.reason
    if result.exit_code == 7:
        assert result.stderr == "registry unavailable"
    assert not (repo / "node_modules" / ".repocli-dependencies.json").exists()


def test_external_dependency_symlink_is_not_reused(repo, tmp_path):
    node(repo)
    other = tmp_path / "other"
    other.mkdir()
    (repo / "node_modules").symlink_to(other, target_is_directory=True)
    (environment,) = inspect_dependencies(repo)
    assert environment.status == "unsupported"
    assert not prepare_dependencies(environment).executed


def test_cancellation_and_timeout_do_not_report_readiness(repo, tmp_path, monkeypatch):
    node(repo)
    child = "import time,pathlib; time.sleep(0.4); pathlib.Path('late-effect').write_text('bad')"
    installer(
        tmp_path,
        monkeypatch,
        body=(
            f"import subprocess; subprocess.Popen([sys.executable, '-c', {child!r}]); time.sleep(5)"
        ),
    )
    (environment,) = inspect_dependencies(repo)
    cancel = Event()
    with operation(cancel=cancel):
        cancel.set()
        result = prepare_dependencies(environment)
        assert result.status == "failed" and not result.executed
    result = prepare_dependencies(environment, timeout=0.2)
    assert result.status == "uncertain" and result.executed
    time.sleep(0.5)
    assert not (repo / "late-effect").exists()
    assert inspect_dependencies(repo)[0].status == "missing"


def test_declared_manager_wins_and_ambiguous_managers_are_not_guessed(repo):
    node(repo)
    (repo / "package-lock.json").write_text("{}")
    assert inspect_dependencies(repo)[0].manager == "bun"
    (repo / "package.json").write_text('{"name":"ambiguous"}')
    with pytest.raises(ValueError, match="Ambiguous"):
        inspect_dependencies(repo)
