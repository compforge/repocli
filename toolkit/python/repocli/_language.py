"""Filename metadata generated from pinned CodeGraph, without loading a parser."""

import json
import posixpath
from importlib.resources import files

_CATALOG: dict[str, dict[str, str]] = json.loads(
    files("repocli").joinpath("languages.json").read_text(encoding="utf-8")
)


def language(path: str) -> str:
    base = posixpath.basename(path)
    ext = ("." + base.rsplit(".", 1)[1]).lower() if "." in base else ""
    if ext in _CATALOG["overrides"]:
        return _CATALOG["overrides"][ext]
    if base in _CATALOG["filenames"]:
        return _CATALOG["filenames"][base]
    suffixes = [base[i:] for i in range(len(base) - 1, 0, -1) if base[i] == "."][:4]
    for suffix in reversed(suffixes):
        if suffix in _CATALOG["registry"]:
            return _CATALOG["registry"][suffix]
    return _CATALOG["fallback"].get(ext, "")
