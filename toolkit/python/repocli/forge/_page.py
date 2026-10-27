"""Bounded repository page positions; provider URLs never come from a cursor."""

from __future__ import annotations

import json

from .model import ForgeError


def page_position(scope: str, limit: int | None, cursor: str | None) -> tuple[int, int]:
    page = 1
    size = 50 if limit is None else limit
    if cursor is not None:
        try:
            value = json.loads(cursor)
        except (ValueError, TypeError) as exc:
            raise ForgeError("Invalid repository page cursor") from exc
        if (
            not isinstance(value, dict)
            or value.get("version") != 1
            or value.get("scope") != scope
            or (limit is not None and limit != value.get("limit"))
        ):
            raise ForgeError("Repository page cursor does not match this query")
        page, size = value.get("page", 0), value.get("limit", 0)
    if type(page) is not int or page < 1 or type(size) is not int or not 1 <= size <= 100:
        raise ForgeError("Repository page requires a positive page and limit between 1 and 100")
    return page, size


def page_continuation(scope: str, page: int, limit: int, more: bool) -> str | None:
    if not more:
        return None
    return json.dumps({"version": 1, "scope": scope, "page": page + 1, "limit": limit})
