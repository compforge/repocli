import type { PullRequest } from "@compforge/harness-common";
import { repository } from "../model.js";
import type { PullRequestReference } from "./model.js";

export function reference(forge: string, path: string, number: number): Pick<PullRequest, "repository" | "number"> {
  return { repository: repository(forge, path), number };
}

/** Validate caller identity before I/O; a remote absence never establishes a lifecycle. */
export function assertReference(value: PullRequestReference, forge: string): void {
  if (value.repository.forge.name !== forge) throw new Error(`PullRequest forge ${value.repository.forge.name} does not match ${forge}`);
  if (!Number.isSafeInteger(value.number) || value.number < 1) throw new Error("PullRequest number must be a positive safe integer");
  if (!value.repository.path || value.repository.path.split("/").some(p => !p || p === "." || p === "..")) throw new Error("Invalid PullRequest repository path");
}
