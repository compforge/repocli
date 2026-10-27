import type { RepositoryKey, RepositorySnapshot, PullRequestSnapshot, PullRequestKey, PullRequestState, PullRequestMergeReadiness } from "@compforge/harness-common";
export type { PullRequestKey } from "@compforge/harness-common";

/** Account discovery and repository membership are different query scopes. Authors are platform usernames. */
export type PullRequestScope =
  | { readonly kind: "account"; readonly author: string }
  | { readonly kind: "repository"; readonly repository: RepositoryKey; readonly author?: string };

export interface PageQuery {
  readonly cursor?: string;
  /** Maximum rows per provider request, 1..100; default 50. */
  readonly pageSize?: number;
}

/** Successful rows remain usable when individual rows cannot be decoded. Issues preclude full coverage. */
export interface Page<T> {
  readonly items: readonly T[];
  readonly nextCursor?: string;
  readonly issues?: readonly ForgeReadIssue[];
}

export interface PullRequestQuery extends PageQuery {
  readonly scope: PullRequestScope;
  readonly state?: PullRequestState | "all";
  readonly updatedAfter?: string;
  readonly updatedBefore?: string;
}

export type PullRequestScanCursor = string & { readonly __pullRequestScanCursor: unique symbol };
export interface PullRequestScanQuery {
  readonly scope: PullRequestScope;
  readonly cursor?: PullRequestScanCursor;
  /** Initial history horizon, default seven days. The initial open inventory is also enumerated. */
  readonly initialSince?: string;
  readonly tracked?: readonly PullRequestKey[];
}
export interface PullRequestScanResult {
  readonly items: readonly PullRequestSnapshot[];
  readonly cursor: PullRequestScanCursor;
  /** More work in this pass. False permits the caller's normal poll interval, even with unresolved issues. */
  readonly pending: boolean;
  /** No unfinished work or retained gaps in this scan. This does not promise a complete event log. */
  readonly complete: boolean;
  readonly issues: readonly ForgeReadIssue[];
}
export interface ForgeReadIssue {
  readonly stage: "list" | "get" | "retry" | "refresh";
  readonly kind: "auth" | "not_found" | "rate_limit" | "transport" | "invalid_data" | "coverage";
  readonly message: string;
  readonly key?: PullRequestKey;
  readonly scope?: PullRequestScope;
  readonly window?: { readonly state: "open" | "all"; readonly updatedAfter: string; readonly updatedBefore: string };
  readonly retryAfterMs?: number;
}

export type PullRequestReviewThreadState = "none" | "unresolved" | "resolved" | "unknown";
/** Review is fetched explicitly; failure never retracts a separately obtained PullRequestSnapshot. */
export interface PullRequestReviewObservation {
  readonly key: PullRequestKey;
  readonly reviewThreadState: PullRequestReviewThreadState;
  readonly reviewActivityFingerprint?: string;
  readonly mergeReadiness?: PullRequestMergeReadiness;
  readonly observedAt: string;
}

/** Read-only provider access. Consumers own polling, snapshot baselines and durable admission. */
export interface ForgeReader {
  readonly forge: string;
  readonly provider: "github" | "gitlab";
  listRepositories(query?: PageQuery): Promise<Page<RepositorySnapshot>>;
  listPullRequests(query: PullRequestQuery): Promise<Page<PullRequestKey>>;
  scanPullRequests(query: PullRequestScanQuery): Promise<PullRequestScanResult>;
  getPullRequest(key: PullRequestKey): Promise<PullRequestSnapshot>;
  getPullRequestReview(key: PullRequestKey): Promise<PullRequestReviewObservation>;
  listPullRequestComments(key: PullRequestKey): Promise<readonly ForgeComment[]>;
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
  readonly resolution?: "unsupported" | "unresolved" | "resolved";
}

/** Connection identity only. Query authors never come from credentials or connection configuration. */
export interface ForgeReaderConfig {
  readonly forge: string;
  readonly provider: "github" | "gitlab";
  readonly host: string;
  readonly token: string;
}
export type ForgeFetch = (input: string | URL | Request, init?: RequestInit) => Promise<Response>;
export interface ForgeReaderOptions {
  readonly fetch?: ForgeFetch;
  readonly now?: () => Date;
  /** Per-request deadline including body consumption; default 10 seconds. */
  readonly timeoutMs?: number;
  /** Maximum response body; default 4 MiB. */
  readonly maxResponseBytes?: number;
}
export class ForgeError extends Error {}
export class ForgeCoverageError extends ForgeError {}
export class ForgeAuthError extends ForgeError {}
export class ForgeNotFound extends ForgeError {}
export class ForgeRateLimitError extends ForgeError {
  constructor(message: string, readonly retryAfterMs: number) { super(message); this.name = "ForgeRateLimitError"; }
}
export function isForgeRateLimitError(error: unknown): error is ForgeRateLimitError {
  return error instanceof ForgeRateLimitError;
}
