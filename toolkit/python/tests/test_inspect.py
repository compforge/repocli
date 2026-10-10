import json
import shutil
import subprocess
from dataclasses import FrozenInstanceError, asdict
from pathlib import Path
from threading import Event

import pytest
from harness_common import Component, Repository

from repocli import _git, _process, inspect, owner
from repocli._language import language
from repocli._layout import load

CONFORMANCE = Path(__file__).resolve().parents[3] / "conformance" / "inspect"
LAYOUTS = json.loads((CONFORMANCE / "layouts.json").read_text())
GIT = shutil.which("git")


def git(root, *args, input=None):
    return subprocess.run(
        [GIT, "-C", str(root), *args], input=input, text=True, check=True, capture_output=True
    ).stdout.strip()


def write(root, files):
    for name, text in files.items():
        path = root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)


@pytest.fixture
def repo(tmp_path):
    git(tmp_path, "init", "-q", "-b", "main")
    return tmp_path.resolve()


def commit(repo):
    git(repo, "add", ".")
    git(
        repo,
        "-c",
        "user.name=Test",
        "-c",
        "user.email=test@example.com",
        "-c",
        "commit.gpgsign=false",
        "commit",
        "--allow-empty",
        "-qm",
        "fixture",
    )
    return git(repo, "rev-parse", "HEAD")


def projection(layout):
    # Normalize Python values only in conformance tests; production has no CLI DTO layer.
    components = []
    for binding in layout.components:
        item = {
            "repository": asdict(binding.repository),
            "name": binding.name,
            "root": binding.root,
            "products": [asdict(p) for p in binding.products],
        }
        for name in ("language", "description"):
            value = getattr(binding, name)
            if value:
                item[name] = value
        if binding.manifests:
            item["manifests"] = list(binding.manifests)
        if binding.package_tools:
            item["packageTools"] = [
                {
                    "name": t.name,
                    "evidence": list(t.evidence),
                    **({"version": t.version} if t.version else {}),
                }
                for t in binding.package_tools
            ]
        components.append(item)
    return {
        "repository": asdict(layout.repository) if layout.repository else None,
        "components": components,
    }


@pytest.mark.parametrize("item", LAYOUTS, ids=lambda item: item["name"])
def test_shared_layout(item):
    files = {name: text.encode() for name, text in item["files"].items()}
    if item["error"]:
        with pytest.raises(ValueError):
            load(files, item["origin"])
    else:
        assert projection(load(files, item["origin"])) == item["expected"]


@pytest.mark.parametrize("options", [{}, {"staged": True}, {"head": "HEAD"}])
@pytest.mark.parametrize("item", LAYOUTS, ids=lambda item: item["name"])
def test_shared_native_inspection(repo, item, options, monkeypatch):
    write(repo, item["files"])
    if item["origin"]:
        git(repo, "remote", "add", "origin", item["origin"])
    commit(repo)
    original = subprocess.Popen

    def only_git(command, *args, **kwargs):
        assert command[0] == "git"
        assert not kwargs.get("shell")
        return original(command, *args, **kwargs)

    monkeypatch.setattr(subprocess, "Popen", only_git)
    if item["error"]:
        with pytest.raises(ValueError):
            inspect(repo, **options)
        return
    report = inspect(repo, **options)
    assert projection(report) == item["expected"]
    assert report.complete and report.checkout == str(repo)
    for path, name in item["owners"].items():
        component = owner(report, path)
        assert (component.name if component else None) == name


def test_filename_probes():
    probes = json.loads((CONFORMANCE / "languages.json").read_text())
    for path, expected in probes.items():
        assert language(path) == expected, path


def test_common_identity_and_immutable_report(repo):
    write(repo, {"Makefile": ""})
    report = inspect(repo)
    binding = report.components[0]
    assert isinstance(binding, Component)
    assert isinstance(binding.repository, Repository)
    assert binding.products == ()
    with pytest.raises(FrozenInstanceError):
        binding.root = "other"
    assert isinstance(report.components, tuple)


def test_versions_and_revision_expressions(repo):
    fixture = json.loads((CONFORMANCE / "versions.json").read_text())
    write(repo, fixture["commit"])
    first = commit(repo)
    write(repo, fixture["index"])
    git(repo, "add", ".")
    write(repo, fixture["working_tree"])
    for options in ({}, {"staged": True}, {"head": "HEAD"}):
        report = inspect(repo, **options)
        assert report.components[0].language == fixture["expected"][report.input]
        assert report.head == (first if options.get("head") else None)
    commit(repo)
    git(
        repo,
        "-c",
        "user.name=Test",
        "-c",
        "user.email=test@example.com",
        "tag",
        "-a",
        "v1",
        "-m",
        "tag",
        first,
    )
    assert inspect(repo, head="HEAD^").head == first
    assert inspect(repo, head="v1").head == first


def test_worktree_uses_its_index_and_paths(repo, tmp_path_factory):
    write(repo, {"go.mod": "module example/app"})
    commit(repo)
    checkout = tmp_path_factory.mktemp("linked") / "tree"
    git(repo, "worktree", "add", "-q", "-b", "feature", str(checkout))
    write(checkout, {"pyproject.toml": ""})
    git(checkout, "add", ".")
    assert inspect(checkout, staged=True).components[0].language == "python"
    assert inspect(repo, staged=True).components[0].language == "go"
    write(checkout, {"sub/file.py": ""})
    assert inspect(checkout / "sub").checkout == str(checkout.resolve())


def test_ignored_nested_repositories_and_large_source(repo):
    write(
        repo,
        {
            "main.go": "x" * (3 << 20),
            "go.mod": "",
            ".gitignore": "ignored/\ncandidates/*\n!candidates/keep/\n",
        },
    )
    commit(repo)
    write(
        repo,
        {
            "ignored/package.json": "{",
            "candidates/drop/package.json": "{}",
            "candidates/keep/package.json": "{}",
            "child/package.json": "{}",
        },
    )
    git(repo / "child", "init", "-q")
    report = inspect(repo)
    assert [c.root for c in report.components] == [".", "candidates/keep"]
    assert report.components[0].language == "go"
    oid = git(repo, "rev-parse", "HEAD")
    git(repo, "update-index", "--add", "--cacheinfo", f"160000,{oid},child")
    assert len(inspect(repo, staged=True).components) == 1
    # Packed objects use the same metadata-only path.
    git(repo, "gc", "--prune=now")
    assert inspect(repo, head="HEAD").components[0].language == "go"


def test_git_excludes_sources(repo):
    write(repo, {"ignored/package.json": "{}", "local/package.json": "{}", "tracked/go.mod": ""})
    git(repo, "add", "tracked")
    (repo / ".git/info/exclude").write_text("ignored/\n")
    exclusion = repo / ".global-excludes"
    exclusion.write_text("local/\ntracked/\n")
    git(repo, "config", "core.excludesFile", str(exclusion))
    assert [c.root for c in inspect(repo).components] == ["tracked"]


@pytest.mark.parametrize("options", [{}, {"staged": True}, {"head": "HEAD"}])
def test_metadata_size_limit(repo, options):
    write(repo, {"package.json": "x" * ((2 << 20) + 1)})
    commit(repo)
    with pytest.raises(ValueError, match="exceeds"):
        inspect(repo, **options)
    git(repo, "gc", "--prune=now")
    with pytest.raises(ValueError, match="exceeds"):
        inspect(repo, **options)


def test_aggregate_and_file_limits(repo, monkeypatch):
    write(repo, {"a/package.json": " " * 64, "b/package.json": " " * 64})
    commit(repo)
    monkeypatch.setattr(_git, "MAX_TOTAL_BYTES", 127)
    for options in ({}, {"staged": True}, {"head": "HEAD"}):
        with pytest.raises(ValueError, match="128 MiB"):
            inspect(repo, **options)
    monkeypatch.setattr(_git, "MAX_FILES", 1)
    with pytest.raises(ValueError, match="10000 files"):
        inspect(repo, staged=True)


def test_symlink_boundaries(repo):
    write(repo, {"outside": "{}"})
    (repo / "source.py").symlink_to("outside")
    assert inspect(repo).components == ()
    (repo / "package.json").symlink_to("outside")
    commit(repo)
    for options in ({}, {"staged": True}, {"head": "HEAD"}):
        with pytest.raises(ValueError, match="symlink|regular file"):
            inspect(repo, **options)


def test_symlinked_ancestor(repo):
    write(repo, {"nested/package.json": "{}", "outside/package.json": "{}"})
    commit(repo)
    shutil.rmtree(repo / "nested")
    (repo / "nested").symlink_to("outside")
    with pytest.raises(ValueError, match="symlinked directory"):
        inspect(repo)
    assert inspect(repo, staged=True).complete


def test_unmerged_index(repo):
    write(repo, {"main.go": "package main"})
    commit(repo)
    oid = git(repo, "rev-parse", "HEAD:main.go")
    git(
        repo,
        "update-index",
        "--index-info",
        input=f"0 {'0' * 40}\tmain.go\n100644 {oid} 1\tmain.go\n100644 {oid} 2\tmain.go\n",
    )
    for options in ({}, {"staged": True}):
        with pytest.raises(ValueError, match="unmerged"):
            inspect(repo, **options)


def test_errors_do_not_fabricate_reports(repo):
    with pytest.raises(ValueError, match="mutually exclusive"):
        inspect(repo, head="HEAD", staged=True)
    for timeout in (0, -1, float("nan"), float("inf")):
        with pytest.raises(ValueError, match="timeout"):
            inspect(repo, timeout=timeout)
    with pytest.raises(subprocess.CalledProcessError):
        inspect(repo, head="missing")
    with pytest.raises(FileNotFoundError):
        inspect(repo / "missing")


def test_catalog_change_retains_first_observation(repo, monkeypatch):
    write(repo, {"first/Makefile": ""})
    original = _git.Git.catalog
    calls = 0

    def change(self, head, staged):
        nonlocal calls
        result = original(self, head, staged)
        calls += 1
        if calls == 1:
            write(repo, {"second/Makefile": ""})
        return result

    monkeypatch.setattr(_git.Git, "catalog", change)
    report = inspect(repo)
    assert not report.complete
    assert report.diagnostics[0].code == "inspection_changed"
    assert report.components[0].name == "first"


def test_cooperative_cancellation_and_deadline(repo, monkeypatch):
    event = Event()
    event.set()
    with pytest.raises(InterruptedError):
        inspect(repo, cancel=event)
    event.clear()
    original = _git.Git.catalog

    def cancel(self, head, staged):
        result = original(self, head, staged)
        event.set()
        return result

    monkeypatch.setattr(_git.Git, "catalog", cancel)
    with pytest.raises(InterruptedError):
        inspect(repo, cancel=event)
    clock = iter((0.0, 0.0, 2.0))
    monkeypatch.setattr(_process.time, "monotonic", lambda: next(clock))
    with pytest.raises(TimeoutError):
        inspect(repo, timeout=1)


def test_inspection_is_read_only(repo):
    write(repo, {"go.mod": "module example/app"})
    commit(repo)
    before = {p.relative_to(repo): p.read_bytes() for p in repo.rglob("*") if p.is_file()}
    for options in ({}, {"staged": True}, {"head": "HEAD"}):
        assert inspect(repo, **options).complete
    after = {p.relative_to(repo): p.read_bytes() for p in repo.rglob("*") if p.is_file()}
    assert after == before


@pytest.mark.parametrize(
    "data", [b'{"unknown":NaN}', b'{"unknown":Infinity}', "{}".encode("utf-16")]
)
def test_legacy_configuration_is_not_decoded(data):
    assert load({".repocli.json": data}, "").components == ()
