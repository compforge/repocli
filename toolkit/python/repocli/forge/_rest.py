"""RestClient — the single HTTP transport shared by every forge adapter.

The GitHub and GitLab REST surfaces differ only in base URL, auth header, and JSON
shapes; the request mechanics (urllib, params encoding, JSON body, error typing,
timeouts) are identical. So this is the ONE place that touches urllib — swap it for the
SDK / an MCP server later and the adapters stay put. Adapters parametrize it with their
base URL + headers and speak verbs (`get/post/put/patch`); they never import urllib.
"""

from __future__ import annotations

import json
import logging
import time
import urllib.error
import urllib.parse
import urllib.request
from contextlib import nullcontext
from email.message import Message
from email.utils import parsedate_to_datetime
from typing import Any

from ..budget import current_budget, operation
from ._http_state import HttpState
from .model import ForgeAuthError, ForgeError, ForgeNotFound, ForgeOutcomeUnknown, ForgeRateLimited

DEFAULT_TIMEOUT = 10
logger = logging.getLogger(__name__)


class RestClient:
    def __init__(
        self,
        base_url: str,
        headers: dict[str, str],
        *,
        timeout: int = DEFAULT_TIMEOUT,
        state: HttpState | None = None,
        resource: str = "core",
    ):
        self.base_url = base_url.rstrip("/")
        self._headers = dict(headers)
        self.timeout = timeout
        self.state, self.resource = state, resource
        self.requests = 0
        self.not_modified = 0

    def request(
        self,
        method: str,
        path: str,
        *,
        params: dict[str, Any] | None = None,
        body: dict[str, Any] | None = None,
    ) -> Any:
        """`<base_url>/<path>`, returns parsed JSON (None on empty body).

        `params` list values encode as repeated keys (e.g. GitLab `iids[]`).
        Conditional responses are revalidated on every read. Cooldown is checked
        under the dispatch lock; no automatic retries, including for mutations.
        """
        url = (
            f"{self.base_url}/{path.lstrip('/')}" if path else self.base_url
        )  # "" → repo root, no trailing slash
        if params:
            url += "?" + urllib.parse.urlencode(params, doseq=True)
        data = json.dumps(body).encode("utf-8") if body is not None else None
        headers = dict(self._headers)
        if data is not None:
            headers.setdefault("Content-Type", "application/json")
        budget = current_budget(self.timeout)
        # A timeout/cancellation before dispatch is a known non-action.
        budget.remaining()
        mutating = method.upper() not in ("GET", "HEAD", "OPTIONS")
        if (
            body
            and isinstance(body.get("query"), str)
            and body["query"].lstrip().startswith("query")
        ):
            mutating = False
        dispatched = False
        try:
            with self.state.acquire(budget) if self.state else nullcontext():
                if self.state:
                    self.state.check(self.resource)
                key = HttpState.cache_key(url, headers)
                cached = self.state.read(key) if self.state and method.upper() == "GET" else {}
                if cached.get("etag"):
                    headers["If-None-Match"] = cached["etag"]
                req = urllib.request.Request(url, data=data, headers=headers, method=method.upper())
                try:
                    timeout = budget.remaining()
                    self.requests += 1
                    dispatched = True
                    with urllib.request.urlopen(req, timeout=timeout) as resp:
                        raw = resp.read((16 << 20) + 1)
                        budget.remaining()
                        if len(raw) > 16 << 20:
                            raise ValueError("response exceeds 16 MiB")
                        value = json.loads(raw) if raw else None
                        limit = self._limit(resp.headers, value, graphql=body is not None)
                        if limit:
                            if self.state:
                                self.state.defer(limit)
                            # A successful final request is usable even when remaining=0.
                            if isinstance(value, dict) and value.get("errors"):
                                raise limit
                        if self.state and method.upper() == "GET" and resp.headers.get("ETag"):
                            self.state.write(key, {"etag": resp.headers["ETag"], "body": value})
                        if self.state:
                            self.state.succeeded()
                        logger.info(
                            "forge request method=%s path=%s resource=%s status=%s",
                            method,
                            path,
                            self.resource,
                            resp.status,
                        )
                        return value
                except urllib.error.HTTPError as e:
                    if e.code == 304 and "body" in cached:
                        limit = self._limit(e.headers, {})
                        if limit and self.state:
                            self.state.defer(limit)
                        self.not_modified += 1
                        if self.state:
                            self.state.succeeded()
                        logger.info(
                            "forge request method=%s path=%s resource=%s status=304",
                            method,
                            path,
                            self.resource,
                        )
                        return cached["body"]
                    raw_error = e.read(65536)
                    try:
                        value = json.loads(raw_error)
                    except ValueError:
                        value = {}
                    limit = self._limit(e.headers, value, status=e.code)
                    if limit:
                        if self.state:
                            self.state.defer(limit)
                        logger.warning(
                            "forge cooldown path=%s resource=%s retry_at=%s secondary=%s",
                            path,
                            limit.resource,
                            limit.retry_at,
                            limit.secondary,
                        )
                        raise limit from e
                    if e.code in (401, 403):
                        raise ForgeAuthError(f"{method} {path} → HTTP {e.code}") from e
                    if e.code == 404:
                        raise ForgeNotFound(f"{method} {path} → 404") from e
                    error = ForgeOutcomeUnknown if mutating and e.code >= 500 else ForgeError
                    raise error(
                        f"{method} {path} → HTTP {e.code}"
                        + (
                            "; inspect remote state before retrying"
                            if error is ForgeOutcomeUnknown
                            else ""
                        )
                    ) from e
        except (urllib.error.URLError, TimeoutError, OSError, ValueError) as e:
            error = ForgeOutcomeUnknown if mutating and dispatched else ForgeError
            raise error(
                f"{method} {path} → {e}"
                + ("; inspect remote state before retrying" if error is ForgeOutcomeUnknown else "")
            ) from e

    def _limit(
        self, headers: Message, value: Any, *, status: int = 200, graphql: bool = False
    ) -> ForgeRateLimited | None:
        remaining = headers.get("x-ratelimit-remaining")
        reset = headers.get("x-ratelimit-reset")
        retry = headers.get("retry-after")
        message = str(value.get("message", "")).lower() if isinstance(value, dict) else ""
        errors = (value.get("errors") or []) if graphql and isinstance(value, dict) else []
        limited = any(isinstance(e, dict) and e.get("type") == "RATE_LIMITED" for e in errors)
        if (
            remaining != "0"
            and not limited
            and not (status in (403, 429) and (status == 429 or retry or "rate limit" in message))
        ):
            return None
        now = time.time()
        retry_at = now + 60
        if reset and remaining == "0":
            retry_at = max(now + 1, float(reset))
        if retry:
            try:
                retry_at = max(retry_at if remaining == "0" else now, now + float(retry))
            except ValueError:
                retry_at = max(retry_at, parsedate_to_datetime(retry).timestamp())
        return ForgeRateLimited(
            headers.get("x-ratelimit-resource", self.resource),
            retry_at,
            secondary=remaining != "0" and not limited,
        )

    def get(self, path: str, **params: Any) -> Any:
        return self.request("GET", path, params=params or None)

    def get_all(self, path: str, *, per_page: int = 100, **params: Any) -> list[Any]:
        """Fetch every page from a list endpoint using the page/per_page convention shared
        by GitHub and GitLab. Keeping the loop here makes "all" a transport guarantee instead
        of an adapter promise that silently stops at its first page."""
        if per_page <= 0 or per_page > 100:
            raise ValueError("per_page must be between 1 and 100")
        with operation(timeout=self.timeout):
            out: list[Any] = []
            page = 1
            while True:
                current_budget(self.timeout).remaining()
                batch = self.get(path, **params, page=page, per_page=per_page)
                if not isinstance(batch, list):
                    raise ForgeError(f"GET {path} page {page}: expected a list response")
                out.extend(batch)
                if len(out) > 10_000:
                    raise ForgeError(f"GET {path}: inventory exceeds 10000 entries")
                if len(batch) < per_page:
                    return out
                page += 1

    def post(self, path: str, body: dict[str, Any]) -> Any:
        return self.request("POST", path, body=body)

    def put(self, path: str, body: dict[str, Any]) -> Any:
        return self.request("PUT", path, body=body)

    def patch(self, path: str, body: dict[str, Any]) -> Any:
        return self.request("PATCH", path, body=body)
