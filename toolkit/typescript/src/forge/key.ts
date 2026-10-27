import type { PullRequestKey } from "@compforge/harness-common";

export function pullRequestKey(forge: string, path: string, number: number): PullRequestKey {
  return { repository: { forge: { name: forge }, path }, number };
}

/** Validate caller identity before I/O; a remote absence never establishes a lifecycle. */
export function assertPullRequestKey(value: PullRequestKey, forge: string): void {
  if (value.repository.forge.name !== forge) throw new Error(`PullRequest forge ${value.repository.forge.name} does not match ${forge}`);
  if (!Number.isSafeInteger(value.number) || value.number < 1) throw new Error("PullRequest number must be a positive safe integer");
  if (!value.repository.path || value.repository.path.split("/").some(p => !p || p === "." || p === "..")) throw new Error("Invalid PullRequest repository path");
}
