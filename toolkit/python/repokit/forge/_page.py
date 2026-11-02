"""Bounded repository page positions; provider URLs never come from a cursor."""

from __future__ import annotations

import json

from .model import ForgeError


def page_position(scope: str, page_size: int | None, cursor: str | None) -> tuple[int, int]:
    page = 1
    size = 50 if page_size is None else page_size
    if cursor is not None:
        try:
            value = json.loads(cursor)
        except (ValueError, TypeError) as exc:
            raise ForgeError("Invalid repository page cursor") from exc
        if (
            not isinstance(value, dict)
            or value.get("version") != 2
            or value.get("scope") != scope
            or (page_size is not None and page_size != value.get("page_size"))
        ):
            raise ForgeError("Repository page cursor does not match this query")
        page, size = value.get("page", 0), value.get("page_size", 0)
    if type(page) is not int or page < 1 or type(size) is not int or not 1 <= size <= 100:
        raise ForgeError("Repository page requires a positive page and page_size between 1 and 100")
    return page, size


def page_continuation(scope: str, page: int, page_size: int, more: bool) -> str | None:
    if not more:
        return None
    return json.dumps({"version": 2, "scope": scope, "page": page + 1, "page_size": page_size})
