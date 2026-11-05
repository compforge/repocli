"""Process adapter for the Python-owned worktree operation, also used by repocli."""

from __future__ import annotations

import argparse
import json
import logging
import os
from dataclasses import asdict, replace
from pathlib import Path

from .forge import ForgeConnection, PullRequestContext, detect_provider
from .git.remote import remote
from .worktrees import clean_worktrees, load_policy


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Retain recent worktrees and prepared environments"
    )
    parser.add_argument("--repo", default=".")
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument("--json", action="store_true")
    parser.add_argument("--max-count", type=int)
    parser.add_argument("--max-prepared", type=int)
    parser.add_argument("--provider", choices=("github", "gitlab"))
    parser.add_argument("--api-host")
    parser.add_argument("--target-repository", action="append")
    parser.add_argument("--state-dir", type=Path, default=Path.home() / ".repocli")
    args = parser.parse_args()
    logging.basicConfig(level=logging.WARNING)
    context: PullRequestContext | None = None
    try:
        policy = load_policy(args.repo)
        policy = replace(
            policy,
            **{
                name: getattr(args, name)
                for name in ("max_count", "max_prepared")
                if getattr(args, name) is not None
            },
        )
        origin = remote(args.repo)
        diagnostic = ""
        if origin:
            provider = detect_provider(origin.host, args.provider)
            token = (
                os.environ.get("GH_TOKEN", os.environ.get("GITHUB_TOKEN", ""))
                if provider == "github"
                else os.environ.get("GITLAB_TOKEN", "")
            )
            if token:
                connection = ForgeConnection(
                    origin.host, token, provider, args.api_host, args.state_dir
                )
                context = connection.pull_request_context(
                    args.repo,
                    source=origin.path,
                    targets=tuple(args.target_repository or [origin.path]),
                )
            else:
                diagnostic = (
                    "PR status unavailable: no Forge credential; only count retention evaluated"
                )
        report = clean_worktrees(args.repo, context=context, policy=policy, dry_run=args.dry_run)
        if diagnostic:
            report = replace(report, errors=(*report.errors, diagnostic))
        if args.json:
            print(json.dumps(asdict(report)))
        else:
            for action in report.actions:
                print(f"{action.status}: {action.action} {action.path} ({action.reason})")
            print(
                f"Remaining: {report.remaining_count} worktrees, "
                f"{report.remaining_prepared} prepared"
            )
            for error in report.errors:
                print(f"warning: {error}")
        return 1 if report.errors else 0
    except (OSError, ValueError) as exc:
        parser.exit(1, f"worktree clean: {exc}\n")
    finally:
        if context:
            context.observer.close()


if __name__ == "__main__":
    raise SystemExit(main())
