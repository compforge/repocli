"""Caller-selected, credential-isolated HTTP coordination for short-lived processes."""

from __future__ import annotations

import fcntl
import hashlib
import json
import os
import time
from collections.abc import Iterator
from contextlib import contextmanager
from pathlib import Path
from typing import Any

from .._process import Budget
from .model import ForgeRateLimited


class HttpState:
    """Share cooldown and conditional responses, never application freshness policy.

    +spec=`All processes using a state directory and credential serialize dispatch;
    a persisted cooldown is checked after acquiring the lock and before network I/O.`
    +link=component://docs/operations.md
    """

    def __init__(self, directory: Path, authority: str, credential: str):
        identity = hashlib.sha256(f"{authority}\0{credential}".encode()).hexdigest()
        self.home = directory / identity

    @contextmanager
    def acquire(self, budget: Budget) -> Iterator[None]:
        self.home.mkdir(parents=True, exist_ok=True, mode=0o700)
        fd = os.open(self.home / "request.lock", os.O_CREAT | os.O_RDWR, 0o600)
        try:
            while True:
                budget.remaining()
                try:
                    fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
                    break
                except BlockingIOError:
                    time.sleep(min(0.05, budget.remaining()))
            yield
        finally:
            os.close(fd)

    def read(self, name: str) -> dict[str, Any]:
        try:
            value = json.loads((self.home / name).read_text())
        except FileNotFoundError:
            return {}
        if not isinstance(value, dict):
            raise ValueError(f"invalid HTTP state: {name}")
        return value

    def write(self, name: str, value: dict[str, Any]) -> None:
        path = self.home / name
        temporary = path.with_suffix(f".{os.getpid()}.tmp")
        fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        with os.fdopen(fd, "w") as stream:
            json.dump(value, stream)
        temporary.replace(path)

    def check(self, resource: str) -> None:
        limits = self.read("limits.json")
        for key in ("secondary", resource):
            retry_at = float(limits.get(key, 0))
            if retry_at > time.time():
                raise ForgeRateLimited(resource, retry_at, secondary=key == "secondary")

    def defer(self, limit: ForgeRateLimited) -> None:
        limits = self.read("limits.json")
        key = "secondary" if limit.secondary else limit.resource
        if limit.secondary:
            failures = int(limits.get("secondary_failures", 0)) + 1
            # Consecutive secondary limits back off even without Retry-After.
            limit.retry_at = max(
                limit.retry_at, time.time() + min(3600, 60 * 2 ** min(failures - 1, 6))
            )
            limits["secondary_failures"] = failures
        limits[key] = max(float(limits.get(key, 0)), limit.retry_at)
        self.write("limits.json", limits)

    def succeeded(self) -> None:
        limits = self.read("limits.json")
        if limits.pop("secondary_failures", None) is not None:
            self.write("limits.json", limits)

    @staticmethod
    def cache_key(url: str, headers: dict[str, str]) -> str:
        representation = json.dumps([url, sorted(headers.items())], sort_keys=True)
        return hashlib.sha256(representation.encode()).hexdigest() + ".json"
