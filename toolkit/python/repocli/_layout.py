"""Pure organization projection from a version's paths and necessary metadata."""

import json
import posixpath
from collections.abc import Mapping

from harness_common import Forge, Repository

from .model import ComponentBinding, Layout, PackageTool
from .remote import parse_remote_url

Catalog = dict[str, bytes | None]
_EXCLUDED = {
    "node_modules",
    "vendor",
    "venv",
    "env",
    "dist",
    "build",
    "target",
    "__pycache__",
    "testdata",
}
_MARKERS = (
    ("python", "pyproject.toml", "setup.py"),
    ("go", "go.mod"),
    ("node", "package.json"),
    ("rust", "Cargo.toml"),
)


def manifest_ecosystem(path: str) -> str:
    return next(
        (ecosystem for ecosystem, *names in _MARKERS if posixpath.basename(path) in names), ""
    )


def valid_path(name: str) -> bool:
    return (
        name not in ("", ".", "..")
        and "\0" not in name
        and "\\" not in name
        and not name.startswith(("/", "../"))
        and posixpath.normpath(name) == name
        and not name.endswith("/")
    )


def needs_content(name: str) -> bool:
    return posixpath.basename(name) == "package.json"


def _origin(origin: str) -> Repository | None:
    remote = parse_remote_url(origin)
    if remote is None:
        return None
    forge = {"github.com": "github", "gitlab.com": "gitlab"}.get(remote.host, remote.host)
    return Repository(Forge(forge), remote.path)


def _skip(name: str) -> bool:
    return any(part.startswith(".") or part in _EXCLUDED for part in name.split("/")[:-1])


def _detect(files: Catalog, root: str) -> str:
    for ecosystem, *markers in _MARKERS:
        for marker in markers:
            name = posixpath.join(root, marker).removeprefix("./")
            if name not in files:
                continue
            if ecosystem != "node":
                return ecosystem
            text = (files[name] or b"").decode("utf-8", errors="replace").lower()
            tsconfig = posixpath.join(root, "tsconfig.json").removeprefix("./")
            return (
                "typescript"
                if "typescript" in text or "@types/" in text or tsconfig in files
                else "javascript"
            )
    return ""


def _discover(files: Catalog) -> list[str]:
    candidates: dict[str, bool] = {}
    for name in files:
        if _skip(name):
            continue
        root = posixpath.dirname(name) or "."
        if manifest_ecosystem(name):
            candidates[root] = True
        elif posixpath.basename(name) == "Makefile":
            candidates.setdefault(root, False)
    selected: list[str] = []
    for root in sorted(candidates):
        # Makefile orchestration must not hide child project boundaries.
        if not any(
            candidates[parent] and parent != "." and root.startswith(parent + "/")
            for parent in selected
        ):
            selected.append(root)
    return selected


# Match Go's JSON field semantics when reading package-manager evidence.
def _invalid_constant(value: str) -> object:
    raise ValueError(f"invalid JSON constant: {value}")


def _json(data: bytes) -> object:
    # Match UTF-8 JSON in Go/TS, not Python's UTF-16 auto-detection or NaN extension.
    return json.loads(data.decode("utf-8", errors="replace"), parse_constant=_invalid_constant)


def _object(value: object) -> dict[str, object]:
    if value is None:
        return {}
    if not isinstance(value, dict) or any(not isinstance(key, str) for key in value):
        raise ValueError("expected an object")
    return value


def _field(value: Mapping[str, object], key: str, default: object = None) -> object:
    return next(
        (value[name] for name in reversed(list(value)) if name.lower() == key.lower()), default
    )


def _string(value: object) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("expected a string")
    return value


def _tools(files: Catalog, root: str) -> tuple[PackageTool, ...]:
    found: dict[str, PackageTool] = {}

    def add(name: str, version: str, evidence: str) -> None:
        old = found.get(name, PackageTool(name))
        found[name] = PackageTool(name, (*old.evidence, evidence), version or old.version)

    manifest = posixpath.join(root, "package.json").removeprefix("./")
    if data := files.get(manifest):
        try:
            metadata = _object(_json(data))
            manager = _string(_field(metadata, "packageManager"))
            name, _, version = manager.partition("@")
            if name in ("npm", "pnpm", "yarn", "bun"):
                add(name, version, manifest + "#packageManager")
        except (ValueError, UnicodeError):
            pass  # Malformed package.json remains a marker; lockfile evidence survives.
    for file, tool in (
        ("go.mod", "go"),
        ("uv.lock", "uv"),
        ("poetry.lock", "poetry"),
        ("Pipfile.lock", "pipenv"),
        ("package-lock.json", "npm"),
        ("npm-shrinkwrap.json", "npm"),
        ("pnpm-lock.yaml", "pnpm"),
        ("yarn.lock", "yarn"),
        ("bun.lock", "bun"),
        ("bun.lockb", "bun"),
    ):
        name = posixpath.join(root, file).removeprefix("./")
        if name in files:
            add(tool, "", name)
    return tuple(
        PackageTool(t.name, tuple(sorted(t.evidence)), t.version) for _, t in sorted(found.items())
    )


def load(files: Catalog, origin: str) -> Layout:
    repo = _origin(origin)
    bindings = tuple(
        ComponentBinding(
            repository=repo or Repository(Forge(""), ""),
            name=root,
            root=root,
            products=(),
            language=_detect(files, root) or None,
            package_tools=_tools(files, root),
            manifests=tuple(
                sorted(
                    name
                    for name in files
                    if (posixpath.dirname(name) or ".") == root and manifest_ecosystem(name)
                )
            ),
        )
        for root in _discover(files)
    )
    return Layout(repo, bindings)
