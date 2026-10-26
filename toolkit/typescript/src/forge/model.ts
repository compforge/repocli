import type { PullRequestSnapshot, PullRequestKey, PullRequestConflictState, PullRequestMergeReadiness } from "@compforge/harness-common";
export type { PullRequestKey } from "@compforge/harness-common";
export type PullRequestReviewThreads = "none" | "unresolved" | "resolved" | "unknown";
/** Opaque, JSON-safe reader checkpoint. Applications must not interpret its contents. */
export type PullRequestCursor = string & { readonly __pullRequestCursor: unique symbol };
export interface PullRequestReadQuery {
  readonly author: string;
  readonly cursor?: PullRequestCursor;
  /** Initial history horizon; defaults to seven days. Existing open PRs are always discovered. */
  readonly since?: string;
  /** Known proposals to read independently of discovery, including after leaving the open inventory. */
  readonly tracked?: readonly PullRequestKey[];
}
export interface PullRequestReadResult {
  readonly items: readonly PullRequestObservation[];
  readonly cursor: PullRequestCursor;
  /** Continue soon to drain the bounded read; otherwise resume on the application's next poll. */
  readonly pending: boolean;
  /** Coverage/read failures retain the affected progress; successful facts remain consumable. */
  readonly errors: readonly string[];
}

/**
 * Serializable platform facts plus one bounded read of transient review/merge conditions.
 * @spec Reader results contain data only and retain their shape through JSON or structured cloning.
 * @spec PullRequest identity and platform facts come from harness-common; review errors never mean resolved/ready.
 * @why Collection time, review activity and merge conditions expire independently of proposal identity.
 * @see {@link component://docs/operations.md}
 */
export interface PullRequestObservation extends PullRequestSnapshot {
  readonly title: string;
  readonly url: string;
  readonly state: "open" | "merged" | "closed";
  readonly reviewThreads: PullRequestReviewThreads;
  readonly reviewActivityKey?: string;
  readonly reviewError?: string;
  readonly conflictState: PullRequestConflictState;
  readonly mergeReadiness: PullRequestMergeReadiness;
  readonly observedAt: string;
}

export interface PullRequestListQuery {
  readonly state: "open" | "merged";
  readonly limit: number;
}
export interface PullRequestDiscoveryQuery {
  /** Explicit platform username; credentials do not define the observed author. */
  readonly author: string;
  readonly state: "open" | "all";
  readonly updatedAfter?: string;
  readonly updatedBefore: string;
  readonly cursor?: string;
}
export interface PullRequestPage {
  readonly items: readonly PullRequestKey[];
  readonly nextCursor?: string;
}

/** A read-only Forge instance. Consumers own polling, checkpoints and admission policy. */
export interface ForgeReader {
  readonly forge: string;
  readonly provider: "github" | "gitlab";
  readPullRequests(query: PullRequestReadQuery): Promise<PullRequestReadResult>;
  /** Raw provider pagination; use readPullRequests for resumable coverage of changing result sets. */
  discover(query: PullRequestDiscoveryQuery): Promise<PullRequestPage>;
  list(path: string, query: PullRequestListQuery): Promise<readonly PullRequestKey[]>;
  get(reference: PullRequestKey): Promise<PullRequestObservation>;
  comments(reference: PullRequestKey): Promise<readonly ForgeComment[]>;
}

/** One provider-neutral conversation comment or diff review thread. */
export interface ForgeComment {
  readonly id: string;
  readonly body: string;
  readonly author?: string;
  readonly replyable: boolean;
  readonly replies: readonly ForgeComment[];
  readonly path?: string;
  readonly line?: number;
  readonly createdAt: string;
}

/** Explicit connection configuration; environment/config-file precedence belongs to the application. */
export interface ForgeReaderConfig {
  readonly forge: string;
  readonly provider: "github" | "gitlab";
  readonly host: string;
  readonly token: string;
  /** Optional legacy author login/id filter for repository-local list; discovery uses its explicit author. */
  readonly uids?: readonly string[];
}
export type ForgeFetch = (input: string | URL | Request, init?: RequestInit) => Promise<Response>;
export interface ForgeReaderOptions {
  readonly fetch?: ForgeFetch;
  readonly now?: () => Date;
  /** Per-request deadline, including response consumption; default 10 seconds. */
  readonly timeoutMs?: number;
  /** Default 4 MiB per response; over-limit reads reject. */
  readonly maxResponseBytes?: number;
}

export class ForgeError extends Error {}
export class ForgeDiscoveryOverflowError extends ForgeError {}
export class ForgeAuthError extends ForgeError {}
export class ForgeNotFound extends ForgeError {}

export class ForgeRateLimitError extends ForgeError {
  constructor(message: string, readonly retryAfterMs: number) {
    super(message);
    this.name = "ForgeRateLimitError";
  }
}
export function isForgeRateLimitError(error: unknown): error is ForgeRateLimitError {
  return error instanceof ForgeRateLimitError;
}
