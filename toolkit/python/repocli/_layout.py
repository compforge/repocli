"""Pure organization projection from a version's paths and necessary metadata."""

import json
import posixpath
from collections.abc import Mapping

from harness_common import Forge, Product, Repository

from ._language import language
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
    return name == ".repocli.json" or posixpath.basename(name) == "package.json"


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
        if ecosystem == "python":
            markers.append("requirements.txt")
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
    languages = {
        "typescript" if value == "tsx" else value
        for name in files
        if not _skip(name) and (root == "." or name.startswith(root + "/"))
        if (value := language(name))
    }
    return "mixed" if len(languages) > 1 else next(iter(languages), "")


def _discover(files: Catalog) -> list[dict[str, object]]:
    roots = {
        posixpath.dirname(name) or "."
        for name in files
        if not _skip(name) and any(posixpath.basename(name) in markers for _, *markers in _MARKERS)
    }
    selected: list[str] = []
    for root in sorted(roots):
        if not any(parent != "." and root.startswith(parent + "/") for parent in selected):
            selected.append(root)
    return [{"name": root, "root": root} for root in selected]


# Match Go's JSON field matching and null/zero-value semantics at the config boundary.
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


def _array(value: object) -> list[object]:
    if value is None:
        return []
    if not isinstance(value, list):
        raise ValueError("expected an array")
    return value


def _repository(value: object) -> Repository:
    obj = _object(value)
    return Repository(
        Forge(_string(_field(_object(_field(obj, "forge")), "name"))), _string(_field(obj, "path"))
    )


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
    components: list[object] | None = None
    data = files.get(".repocli.json")
    if data is not None:
        try:
            config = _object(_json(data))
            missing = object()
            declared = _field(config, "repository", missing)
            if declared is None:
                repo = None
            elif declared is not missing:
                value = _object(declared)
                host = _field(_object(_field(value, "forge")), "name")
                path = _field(value, "path")
                repo = Repository(
                    Forge(_string(host if host is not None else repo.forge.name if repo else None)),
                    _string(path if path is not None else repo.path if repo else None),
                )
            if repo and (not repo.forge.name or not repo.path):
                raise ValueError("repository requires forge.name and path")
            raw_components = _field(config, "components")
            components = _array(raw_components) if raw_components is not None else None
        except (ValueError, UnicodeError) as error:
            raise ValueError("read .repocli.json") from error
    if components is None:
        components = list(_discover(files))
    roots: set[str] = set()
    names: set[str] = set()
    bindings = []
    for component in components:
        item = _object(component)
        root, name = _string(_field(item, "root")) or ".", _string(_field(item, "name"))
        if not name or name in names or root in roots or root != "." and not valid_path(root):
            raise ValueError("invalid or duplicate component name/root")
        roots.add(root)
        names.add(name)
        products = [_string(_field(_object(v), "name")) for v in _array(_field(item, "products"))]
        if any(not name for name in products) or len(set(products)) != len(products):
            raise ValueError("empty or duplicate product name")
        for raw in _array(_field(item, "packageTools")):
            tool = _object(raw)
            _string(_field(tool, "name"))
            _string(_field(tool, "version"))
            for evidence in _array(_field(tool, "evidence")):
                _string(evidence)
        for manifest in _array(_field(item, "manifests")):
            _string(manifest)
        identity = _repository(_field(item, "repository"))
        bindings.append(
            ComponentBinding(
                repository=repo or identity,
                name=name,
                root=root,
                products=tuple(Product(name) for name in sorted(products)),
                description=_string(_field(item, "description")) or None,
                language=_string(_field(item, "language")) or _detect(files, root) or None,
                package_tools=_tools(files, root),
                manifests=tuple(
                    sorted(
                        name
                        for name in files
                        if (posixpath.dirname(name) or ".") == root and manifest_ecosystem(name)
                    )
                ),
            )
        )
    return Layout(repo, tuple(sorted(bindings, key=lambda binding: binding.root)))
