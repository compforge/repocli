"""Remote identity without provider policy, credentials or network access."""

from dataclasses import dataclass
from pathlib import Path
from urllib.parse import unquote, urlsplit

from .git import git


@dataclass(frozen=True)
class Remote:
    host: str
    path: str


def parse_remote_url(value: str) -> Remote | None:
    """Parse network Git URLs; local/file paths have no remote host identity."""
    if "://" in value:
        try:
            url = urlsplit(value)
            # Identity parsing does not decide which transports Git may execute.
            if url.scheme == "file":
                return None
            host, path = url.hostname or "", unquote(url.path).removeprefix("/")
        except ValueError:
            return None
    elif ":" in value:
        host, path = value.split(":", 1)
        host = host.rsplit("@", 1)[-1]
        if "/" in host or "\\" in host:
            return None
    else:
        return None
    path = path.removesuffix("/").removesuffix(".git")
    return Remote(host.lower(), path) if host and path else None


def remote(repo: str | Path, name: str = "origin") -> Remote | None:
    """Configured fetch remote identity; None if absent or not a network remote."""
    result = git(repo, "remote", "get-url", "--", name)
    return parse_remote_url(result.out) if result.ok else None
