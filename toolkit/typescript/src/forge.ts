export type { PullRequest, PullRequestState } from "@compforge/harness-common";
import type { PullRequest } from "@compforge/harness-common";
export * from "./forge/model.js";
export { GitHubForgeReader } from "./forge/github.js";
export { GitLabForgeReader } from "./forge/gitlab.js";
export { createForgeReader } from "./forge/create.js";

export type CommentResolution = "unsupported" | "unresolved" | "resolved";

/** Provider-neutral conversation or review thread. */
export interface Comment {
  readonly author: string;
  readonly body: string;
  readonly id: string;
  readonly path: string;
  readonly line?: number;
  readonly createdAt: string;
  readonly replies: readonly Comment[];
  readonly replyRef: string;
  readonly resolveRef: string;
  readonly resolution: CommentResolution;
}

export interface Release {
  readonly tag: string;
  readonly name: string;
  readonly target: string;
  readonly webUrl: string;
  readonly createdAt?: string;
}

export type MergeReadiness =
  | "ready"
  | "conflict"
  | "discussions_unresolved"
  | "ci_blocked"
  | "needs_approval"
  | "draft"
  | "unknown";

/** Forge primitives consumed by workflows; provider policy stays outside adapters. */
export interface ForgePort {
  readonly provider: "github" | "gitlab";
  create(input: { readonly sourceBranch: string; readonly targetBranch: string; readonly title: string; readonly body?: string }): Promise<PullRequest>;
  get(number: number): Promise<PullRequest>;
  description(number: number): Promise<string>;
  update(number: number, fields: { readonly title?: string; readonly body?: string; readonly targetBranch?: string }): Promise<PullRequest>;
  close(number: number): Promise<PullRequest>;
  pullRequestsForBranch(branch: string): Promise<readonly PullRequest[]>;
  recent(limit: number): Promise<readonly PullRequest[]>;
  defaultBranch(): Promise<string>;
  createRelease(input: { readonly tag: string; readonly target: string; readonly name?: string; readonly notes?: string }): Promise<Release>;
  latestRelease(): Promise<Release | undefined>;
  comments(number: number): Promise<readonly Comment[]>;
  comment(number: number, body: string, options?: { readonly replyable?: boolean; readonly path?: string; readonly line?: number }): Promise<void>;
  reply(number: number, target: Comment, body: string): Promise<void>;
  resolveComment(number: number, target: Comment): Promise<void>;
  mergeReadiness(number: number): Promise<MergeReadiness>;
}

export function parsePullRequestNumber(value: string): number | undefined {
  const match = value.match(/\/(?:pull|merge_requests)\/(\d+)/) ?? value.trim().match(/^[!#]?(\d+)$/);
  return match?.[1] === undefined ? undefined : Number.parseInt(match[1], 10);
}

