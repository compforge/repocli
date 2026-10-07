"""RestClient — the single HTTP transport shared by every forge adapter.

The GitHub and GitLab REST surfaces differ only in base URL, auth header, and JSON
shapes; the request mechanics (urllib, params encoding, JSON body, error typing,
timeouts) are identical. So this is the ONE place that touches urllib — swap it for the
SDK / an MCP server later and the adapters stay put. Adapters parametrize it with their
base URL + headers and speak verbs (`get/post/put/patch`); they never import urllib.
"""

from __future__ import annotations

import json
import urllib.error
import urllib.parse
import urllib.request
from typing import Any

from ..budget import current_budget, operation
from .model import ForgeAuthError, ForgeError, ForgeNotFound, ForgeOutcomeUnknown

DEFAULT_TIMEOUT = 10


class RestClient:
    def __init__(self, base_url: str, headers: dict[str, str], *, timeout: int = DEFAULT_TIMEOUT):
        self.base_url = base_url.rstrip("/")
        self._headers = dict(headers)
        self.timeout = timeout

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
        Maps 401/403 → ForgeAuthError, 404 → ForgeNotFound, else ForgeError. The
        ONLY HTTP call in the forge layer.
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
        req = urllib.request.Request(url, data=data, headers=headers, method=method.upper())
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
        try:
            with urllib.request.urlopen(req, timeout=budget.remaining()) as resp:
                raw = resp.read((16 << 20) + 1)
                budget.remaining()
                if len(raw) > 16 << 20:
                    raise ValueError("response exceeds 16 MiB")
                return json.loads(raw) if raw else None
        except urllib.error.HTTPError as e:
            if e.code in (401, 403):
                raise ForgeAuthError(f"{method} {path} → HTTP {e.code}") from e
            if e.code == 404:
                raise ForgeNotFound(f"{method} {path} → 404") from e
            error = ForgeOutcomeUnknown if mutating and e.code >= 500 else ForgeError
            raise error(
                f"{method} {path} → HTTP {e.code}"
                + ("; inspect remote state before retrying" if error is ForgeOutcomeUnknown else "")
            ) from e
        except (urllib.error.URLError, TimeoutError, OSError, ValueError) as e:
            error = ForgeOutcomeUnknown if mutating else ForgeError
            raise error(
                f"{method} {path} → {e}"
                + ("; inspect remote state before retrying" if mutating else "")
            ) from e

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
