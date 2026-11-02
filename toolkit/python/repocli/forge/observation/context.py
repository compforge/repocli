"""PR association queries joined to live local branch and checkout topology."""

from __future__ import annotations

from pathlib import Path
from typing import cast

from harness_common import Repository

from ... import git_state
from ...budget import operation
from ...remote import remote
from ..model import ForgeError, PullRequest
from .model import BranchPullRequests, PullRequestKey
from .service import PullRequestObserver, epoch, iso, key_from_json, key_json, repository_json
from .store import JSON, digest


class PullRequestContext:
    """One local repository and explicit remote query scope; no workflow selection policy.

    @spec An association is scoped by source repository, branch and target repositories.
    @spec Multiple matching proposals remain ambiguous; detached checkouts have no branch.
    @spec Git topology is read live and never persisted as authority to remove resources.
    @link component://docs/pr.md
    """

    def __init__(
        self,
        observer: PullRequestObserver,
        checkout: str | Path,
        *,
        source: Repository,
        targets: tuple[Repository, ...],
    ):
        if not targets or any(repo.forge.name != observer.forge for repo in (source, *targets)):
            raise ValueError("source and targets must belong to the observer Forge")
        self.observer, self.checkout = observer, str(checkout)
        self.source, self.targets = source, tuple(dict.fromkeys(targets))
        self._remote = remote(self.checkout)

    def _association_path(self, branch: str) -> Path:
        observer = self.observer
        identity = [
            observer.connection,
            repository_json(self.source),
            branch,
            [repository_json(repo) for repo in self.targets],
        ]
        return observer.store.root / "associations" / f"{digest(identity)}.json"

    def _keys(self, branch: str, *, verify: bool) -> tuple[PullRequestKey, ...]:
        observer = self.observer
        path = self._association_path(branch)
        with observer.store.locked(path) as owned:
            record = observer.store.read(path)
            if record and (
                record.get("version") != 1
                or not record.get("error")
                and not isinstance(record.get("keys"), list)
            ):
                raise ForgeError("invalid PR association record")
            try:
                refresh_at = epoch(record.get("nextRefreshAt")) if record else 0
                cached = (
                    tuple(key_from_json(row) for row in cast(list[JSON], record["keys"]))
                    if record and not record.get("error")
                    else ()
                )
            except (KeyError, TypeError, ValueError) as exc:
                raise ForgeError(f"invalid PR association record for {branch}: {exc}") from exc
            if not verify and record and refresh_at > observer.now():
                if record.get("error"):
                    raise ForgeError(str(record["error"]))
                return cached
            keys: list[PullRequestKey] = []
            try:
                for target in self.targets:
                    for pr in observer.reader.for_repository(target.path).prs_for_branch(
                        branch, source_repository=self.source
                    ):
                        if (
                            pr.source_repository != self.source.path
                            or pr.target_repository != target.path
                            or pr.source_branch != branch
                        ):
                            raise ForgeError("Forge returned a different branch association")
                        keys.append(PullRequestKey(target, pr.number))
            except (ForgeError, OSError) as exc:
                if isinstance(exc, InterruptedError):
                    raise
                observer.store.write(
                    path,
                    {
                        "version": 1,
                        "error": str(exc),
                        "nextRefreshAt": iso(observer.now() + observer.retry_interval),
                    },
                    owned,
                )
                raise
            unique = tuple(dict.fromkeys(keys))
            observer.store.write(
                path,
                {
                    "version": 1,
                    "keys": [key_json(key) for key in unique],
                    "nextRefreshAt": iso(observer.now() + observer.open_interval),
                },
                owned,
            )
            return unique

    def _branch(self, branch: str, *, verify: bool = False) -> BranchPullRequests:
        with operation(timeout=self.observer.timeout, cancel=self.observer._stop):
            heads = dict(git_state.list_local_branches(self.checkout))
            if branch not in heads:
                raise ValueError(f"local branch does not exist: {branch}")
            head = heads[branch]
            checkouts = tuple(
                entry for entry in git_state.list_checkouts(self.checkout) if entry.branch == branch
            )
            candidates: list[PullRequest] = []
            errors: list[str] = []
            try:
                keys = self._keys(branch, verify=verify)
                for key in keys:
                    try:
                        pr = self.observer._snapshot(key, verify=verify)
                        # Metadata belongs to the successful observation, not to a branch name.
                        if not pr.source_repository or not pr.source_branch:
                            raise ForgeError("observed PR source is unknown")
                        if pr.source_branch != branch or pr.source_repository != self.source.path:
                            continue
                        candidates.append(pr)
                    except (ForgeError, OSError) as exc:
                        if isinstance(exc, InterruptedError):
                            raise
                        errors.append(str(exc))
            except (ForgeError, OSError) as exc:
                if isinstance(exc, InterruptedError):
                    raise
                errors.append(str(exc))
            opens = [pr for pr in candidates if pr.is_open]
            matching = list(opens)
            if not opens:
                for pr in candidates:
                    try:
                        if pr.sha and git_state.is_ancestor(self.checkout, pr.sha, head):
                            matching.append(pr)
                    except OSError as exc:
                        if isinstance(exc, InterruptedError):
                            raise
                        errors.append(f"PR {pr.number} ancestry unavailable: {exc}")
            return BranchPullRequests(
                self.source,
                branch,
                head,
                tuple(candidates),
                checkouts,
                matching[0] if len(matching) == 1 and not errors else None,
                len(matching) > 1,
                "; ".join(errors) or None,
            )

    def get_branch(self, branch: str) -> BranchPullRequests:
        return self._branch(branch)

    def get_checkout(self) -> BranchPullRequests | None:
        branch = git_state.get_current_branch(self.checkout)
        return self.get_branch(branch) if branch else None

    def local_branches(self, *, limit: int = 20) -> tuple[BranchPullRequests, ...]:
        """Bound cold discovery; oldest due associations progress independently of retries.

        @spec Per-branch read failures preserve other results; cancellation aborts the round.
        @case tests/test_pr_observation.py
        """
        if limit < 1:
            raise ValueError("limit must be positive")
        branches = git_state.list_local_branches(self.checkout)
        checkouts = git_state.list_checkouts(self.checkout)
        due = {}
        unavailable: dict[str, str] = {}
        for branch, _ in branches:
            try:
                record = self.observer.store.read(self._association_path(branch))
                due[branch] = epoch(record.get("nextRefreshAt")) if record else 0
            except (OSError, ValueError) as exc:
                unavailable[branch] = str(exc)
                due[branch] = float("inf")
        selected = {
            branch
            for branch, at in sorted(due.items(), key=lambda item: (item[1], item[0]))
            if at <= self.observer.now()
        }
        selected = set(sorted(selected, key=lambda branch: (due[branch], branch))[:limit])
        results = []
        with operation(timeout=self.observer.timeout, cancel=self.observer._stop):
            from ...budget import current_budget

            for branch, head in branches:
                error = unavailable.get(branch)
                try:
                    current_budget(self.observer.timeout).remaining()
                    if error:
                        pass
                    elif branch not in selected and due[branch] <= self.observer.now():
                        error = "association discovery pending"
                    else:
                        results.append(self.get_branch(branch))
                        continue
                except TimeoutError:
                    error = "association round timed out"
                except (ForgeError, OSError, ValueError) as exc:
                    if isinstance(exc, InterruptedError):
                        raise
                    error = str(exc)
                results.append(
                    BranchPullRequests(
                        self.source,
                        branch,
                        head,
                        (),
                        tuple(c for c in checkouts if c.branch == branch),
                        None,
                        error=error,
                    )
                )
        return tuple(results)

    def for_pull_request(self, key: PullRequestKey) -> tuple[BranchPullRequests, ...]:
        pr = self.observer._snapshot(key)
        if key.repository not in self.targets or pr.source_repository != self.source.path:
            return ()
        if pr.source_branch not in dict(git_state.list_local_branches(self.checkout)):
            return ()
        branch = self.get_branch(pr.source_branch)
        return (
            (branch,)
            if any(
                p.number == key.number and p.target_repository == key.repository.path
                for p in branch.candidates
            )
            else ()
        )

    def verify(self, previous: BranchPullRequests) -> BranchPullRequests:
        """Recheck a decision's association and identity before the caller mutates resources."""
        if remote(self.checkout) != self._remote:
            raise ForgeError("source repository remote changed during PR decision")
        current = self._branch(previous.branch, verify=True)
        if current.head != previous.head or current.checkouts != previous.checkouts:
            raise ForgeError("local branch or checkout changed during PR decision")
        return current
