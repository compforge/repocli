"""Read-only organization observations built on common source identities."""

from dataclasses import dataclass, field
from typing import Literal

from harness_common import Component, Product, Repository


@dataclass(frozen=True, slots=True)
class PackageTool:
    name: str
    evidence: tuple[str, ...] = ()
    version: str | None = None


@dataclass(frozen=True, slots=True, kw_only=True)
class ComponentBinding(Component):
    """A common Component identity with checkout-relative layout and tool evidence."""

    root: str
    products: tuple[Product, ...] = ()
    description: str | None = field(default=None, compare=False)
    language: str | None = field(default=None, compare=False)
    package_tools: tuple[PackageTool, ...] = ()
    manifests: tuple[str, ...] = ()


@dataclass(frozen=True, slots=True)
class Layout:
    repository: Repository | None
    components: tuple[ComponentBinding, ...]


@dataclass(frozen=True, slots=True)
class Diagnostic:
    code: str
    message: str


@dataclass(frozen=True, slots=True, kw_only=True)
class InspectReport(Layout):
    """Completeness describes path/metadata observation, not buildability or source contents."""

    checkout: str
    input: Literal["working_tree", "index", "commit"]
    complete: bool
    diagnostics: tuple[Diagnostic, ...] = ()
    head: str | None = None


def owner(layout: Layout, path: str) -> ComponentBinding | None:
    """Return the deepest owning root for a repository-relative path, even after deletion."""
    found = None
    for component in layout.components:
        root = component.root
        if root == "." or path == root or path.startswith(root + "/"):
            if found is None or found.root == "." or len(root) > len(found.root):
                found = component
    return found
