import type { PullRequestConflictState, PullRequestMergeReadiness, PullRequestState } from "@compforge/harness-common";
import { assertReference, reference } from "./identity.js";
import { discoveryAuthor, discoveryPage, nextPage } from "./discovery.js";
import type {
  ForgeComment as Comment,
  ForgeReader,
  PullRequestKey,
  PullRequestDiscoveryQuery,
  PullRequestPage,
  PullRequestListQuery,
  PullRequestReadQuery,
  PullRequestObservation,
  ForgeReaderOptions,
  PullRequestReviewThreads,
} from "./model.js";
import { readPullRequests } from "./read.js";
import { ForgeDiscoveryOverflowError, isForgeRateLimitError } from "./model.js";
import type { ForgeReaderConfig } from "./model.js";
import {
  JsonHttpClient,
  nonEmptyString,
  positiveLimit,
  record,
  records,
  reviewActivityKey,
} from "./http.js";

const MAX_REVIEW_THREAD_PAGES = 20;
const MAX_COMMENT_PAGES = 20;
const MAX_PULL_REQUEST_PAGES = 20;
const PULL_REQUEST_PAGE_SIZE = 100;


function repositoryPath(repository: string): string {
  const parts = repository.split("/");
  if (parts.length !== 2 || parts.some((part) => !part || part === "." || part === "..")) {
    throw new Error(
      `GitHub repository must be owner/repo, got ${repository}`,
    );
  }
  return parts.map(encodeURIComponent).join("/");
}

function lifecycle(pullRequest: Record<string, unknown>): PullRequestState {
  if (pullRequest.merged === true || pullRequest.merged_at) return "merged";
  if (pullRequest.state === "open") return "open";
  if (pullRequest.state === "closed") return "closed";
  throw new Error(`unknown GitHub PullRequest state: ${pullRequest.state}`);
}

function conflictState(pr: Record<string, unknown>): PullRequestConflictState {
  return pr.mergeable === true ? "none" : pr.mergeable === false ? "conflicted" : "unknown";
}
function mergeReadiness(pr: Record<string, unknown>): PullRequestMergeReadiness {
  if (pr.isDraft === true) return "draft";
  switch (pr.mergeStateStatus) {
    case "CLEAN": return "ready";
    case "DIRTY": return "conflict";
    case "BLOCKED": return "blocked";
    case "BEHIND": return "behind";
    case "UNSTABLE": return "ci_blocked";
    case "DRAFT": return "draft";
    default: return "unknown";
  }
}

function authorMatches(
  pullRequest: Record<string, unknown>,
  uids: readonly string[],
): boolean {
  const user = pullRequest.user;
  if (!user || typeof user !== "object" || Array.isArray(user)) return false;
  const author = user as Record<string, unknown>;
  return uids.some((uid) =>
    author.login === uid ||
    (author.id !== undefined && String(author.id) === uid)
  );
}

export class GitHubForgeReader implements ForgeReader {
  readonly forge: string;
  readonly provider = "github" as const;
  readonly #token: string;
  readonly #restBase: string;
  readonly #graphqlUrl: string;
  readonly #http: JsonHttpClient;
  readonly #now: () => Date;
  readonly #uids?: readonly string[];

  constructor(
    config: ForgeReaderConfig,
    options: ForgeReaderOptions = {},
  ) {
    if (config.provider !== "github") {
      throw new Error("GitHubForgeReader requires a github config");
    }
    if (!config.token) throw new Error(`GitHub token missing for ${config.host}`);
    this.forge = config.forge;
    this.#token = config.token;
    this.#uids = config.uids;
    const restBase = config.host === "github.com"
      ? "https://api.github.com"
      : `https://${config.host}/api/v3`;
    this.#restBase = restBase;
    this.#graphqlUrl = restBase.endsWith("/api/v3")
      ? `${restBase.slice(0, -"/api/v3".length)}/api/graphql`
      : `${restBase}/graphql`;
    this.#http = new JsonHttpClient(options);
    this.#now = options.now ?? (() => new Date());
  }

  readPullRequests(query: PullRequestReadQuery) { return readPullRequests(this, query, this.#now(), operation => this.#http.withinDeadline(20_000, operation)); }

  async discover(query: PullRequestDiscoveryQuery): Promise<PullRequestPage> {
    const page = discoveryPage(query);
    if (page > 20) throw new Error("GitHub discovery exceeds the 1000-result search window");
    const end = new Date(query.updatedBefore).toISOString();
    const window = query.updatedAfter ? `${new Date(query.updatedAfter).toISOString()}..${end}` : `<=${end}`;
    const q = `is:pr author:${query.author} updated:${window}${query.state === "open" ? " is:open" : ""}`;
    const { data, headers } = await this.#http.request("GET", `${this.#restBase}/search/issues?${new URLSearchParams({ q, sort: "updated", order: "asc", per_page: "50", page: String(page) })}`, { headers: this.#headers() });
    const result = record("GitHub PR discovery", data);
    if (result.incomplete_results !== false || !Number.isSafeInteger(result.total_count) || (Number(result.total_count) < 0 || Number(result.total_count) > 1000)) {
      throw new ForgeDiscoveryOverflowError("GitHub PR discovery incomplete or exceeds 1000 results; checkpoint retained");
    }
    const items = records("GitHub PR discovery items", result.items).map(row => {
      const repository = nonEmptyString("GitHub discovery repository_url", row.repository_url);
      const prefix = `${this.#restBase}/repos/`;
      if (!repository.startsWith(prefix) || !row.pull_request || !Number.isSafeInteger(row.number) || Number(row.number) < 1) throw new Error("Invalid GitHub PR discovery identity");
      const path = repository.slice(prefix.length);
      repositoryPath(path);
      if (!discoveryAuthor(row.user, "login", query.author)) throw new Error("GitHub discovery returned another author's PR");
      return reference(this.forge, path, Number(row.number));
    });
    const cursor = nextPage(headers, page, "github");
    return { items, ...(cursor ? { nextCursor: cursor } : {}) };
  }

  async list(
    path: string,
    query: PullRequestListQuery,
  ): Promise<readonly PullRequestKey[]> {
    const limit = positiveLimit(query.limit);
    return await this.#listByState(
      path,
      query.state === "open" ? "open" : "closed",
      limit,
    );
  }

  async #listByState(
    path: string,
    state: "open" | "closed",
    limit?: number,
  ): Promise<readonly PullRequestKey[]> {
    const result: PullRequestKey[] = [];
    const seen = new Set<number>();
    for (let page = 1; page <= MAX_PULL_REQUEST_PAGES; page += 1) {
      const { data, headers } = await this.#http.request(
        "GET",
        `${this.#restBase}/repos/${repositoryPath(path)}` +
          `/pulls?state=${state}&sort=updated&direction=desc` +
          `&per_page=${
            this.#uids
              ? PULL_REQUEST_PAGE_SIZE
              : Math.min(
                PULL_REQUEST_PAGE_SIZE,
                limit ?? PULL_REQUEST_PAGE_SIZE,
              )
          }` +
          `&page=${page}`,
        { headers: this.#headers() },
      );
      const pullRequests = records("GitHub PullRequests", data);
      for (const [index, pullRequest] of pullRequests.entries()) {
        if (
          (state === "open" && pullRequest.state !== "open") ||
          (state === "closed" && !Boolean(pullRequest.merged_at))
        ) {
          continue;
        }
        if (this.#uids && !authorMatches(pullRequest, this.#uids)) continue;
        const number = pullRequest.number;
        if (!Number.isSafeInteger(number) || (number as number) < 1) {
          throw new Error(`GitHub PullRequests[${index}].number is invalid`);
        }
        if (seen.has(number as number)) continue;
        seen.add(number as number);
        result.push(reference(this.forge, path, number as number));
        if (limit !== undefined && result.length === limit) return result;
      }
      const hasNextPage = headers.get("link")
        ?.split(",")
        .some((link) => /;\s*rel="next"/.test(link)) ?? false;
      if (!hasNextPage) return result;
    }
    throw new Error(
      `GitHub ${state} PullRequest pagination limit exceeded`,
    );
  }

  async get(identity: PullRequestKey): Promise<PullRequestObservation> {
    this.#assertForge(identity);
    const repository = reference(this.forge, identity.repository.path, identity.number).repository;
    const { data } = await this.#http.request(
      "GET",
      `${this.#restBase}/repos/${repositoryPath(identity.repository.path)}` +
        `/pulls/${identity.number}`,
      { headers: this.#headers() },
    );
    const pullRequest = record("GitHub PullRequest", data);
    let readiness: PullRequestMergeReadiness = pullRequest.draft === true ? "draft" : "unknown";
    let reviewThreads: PullRequestReviewThreads = "unknown";
    let reviewActivity: string | undefined;
    let reviewError: string | undefined;
    try {
      const observation = await this.#reviewThreads(identity);
      readiness = observation.readiness;
      reviewThreads = observation.state;
      reviewActivity = observation.activityKey;
    } catch (error) {
      if (isForgeRateLimitError(error)) throw error;
      reviewError = error instanceof Error ? error.message : String(error);
      // Review thread support varies across GitHub Enterprise versions. A
      // partial observation is safer than treating ordinary comments as gates.
    }
    const head = pullRequest.head && typeof pullRequest.head === "object" ? pullRequest.head as Record<string, unknown> : {};
    const base = pullRequest.base && typeof pullRequest.base === "object" ? pullRequest.base as Record<string, unknown> : {};
    const source = head.repo && typeof head.repo === "object" ? head.repo as Record<string, unknown> : {};
    return {
      ...(typeof source.full_name === "string" ? { sourceRepository: reference(this.forge, source.full_name, identity.number).repository } : {}),
      ...(typeof head.ref === "string" ? { sourceBranch: head.ref } : {}),
      ...(typeof base.ref === "string" ? { targetBranch: base.ref } : {}),
      repository,
      number: identity.number,
      title: nonEmptyString("GitHub PullRequest.title", pullRequest.title),
      url: nonEmptyString("GitHub PullRequest.html_url", pullRequest.html_url),
      state: lifecycle(pullRequest),
      reviewThreads,
      ...(reviewError ? { reviewError } : {}),
      ...(reviewActivity ? { reviewActivityKey: reviewActivity } : {}),
      conflictState: conflictState(pullRequest),
      mergeReadiness: readiness,
      ...(typeof (pullRequest.head as Record<string, unknown> | undefined)?.sha === "string" ? { headSha: (pullRequest.head as Record<string, string>).sha } : {}),
      ...(typeof (pullRequest.base as Record<string, unknown> | undefined)?.sha === "string" ? { baseSha: (pullRequest.base as Record<string, string>).sha } : {}),
      ...(typeof pullRequest.updated_at === "string" ? { updatedAt: pullRequest.updated_at } : {}),
      ...(typeof pullRequest.merged_at === "string" ? { mergedAt: pullRequest.merged_at } : {}),
      observedAt: this.#now().toISOString(),
    };
  }

  async comments(
    identity: PullRequestKey,
  ): Promise<readonly Comment[]> {
    this.#assertForge(identity);
    const base =
      `${this.#restBase}/repos/${repositoryPath(identity.repository.path)}`;
    const [conversation, review] = await Promise.all([
      this.#commentRows(`${base}/issues/${identity.number}/comments`),
      this.#commentRows(`${base}/pulls/${identity.number}/comments`),
    ]);
    const toComment = (
      row: Record<string, unknown>,
      anchored: boolean,
      index: number,
    ): Comment => {
      const id = row.id;
      if (
        (typeof id !== "number" && typeof id !== "string") ||
        id === ""
      ) {
        throw new Error(`GitHub comments[${index}].id is invalid`);
      }
      const createdAt = nonEmptyString(
        `GitHub comments[${index}].created_at`,
        row.created_at,
      );
      return {
        id: String(id),
        body: typeof row.body === "string" ? row.body : "",
        ...(typeof (row.user as Record<string, unknown> | undefined)
              ?.login === "string"
          ? {
            author: (row.user as Record<string, unknown>).login as string,
          }
          : {}),
        replyable: false,
        replies: [],
        ...(anchored && typeof row.path === "string"
          ? { path: row.path }
          : {}),
        ...(anchored &&
            Number.isSafeInteger(row.line ?? row.original_line)
          ? { line: (row.line ?? row.original_line) as number }
          : {}),
        createdAt,
      };
    };

    const comments = conversation.map((row, index) =>
      toComment(row, false, index)
    );
    const replies = new Map<string, Comment[]>();
    const roots: Comment[] = [];
    for (const [index, row] of review.entries()) {
      const comment = toComment(row, true, conversation.length + index);
      const parent = row.in_reply_to_id;
      if (parent !== undefined && parent !== null) {
        const parentId = String(parent);
        const siblings = replies.get(parentId);
        if (siblings) {
          siblings.push(comment);
        } else {
          replies.set(parentId, [comment]);
        }
      } else {
        roots.push(comment);
      }
    }
    comments.push(...roots.map((root) => ({
      ...root,
      replyable: true,
      replies: (replies.get(root.id) ?? []).sort((left, right) =>
        left.createdAt.localeCompare(right.createdAt)
      ),
    })));
    return comments.sort((left, right) =>
      left.createdAt.localeCompare(right.createdAt)
    );
  }

  #headers(): HeadersInit {
    return {
      Accept: "application/vnd.github+json",
      Authorization: `Bearer ${this.#token}`,
      "Content-Type": "application/json",
      "X-GitHub-Api-Version": "2022-11-28",
    };
  }

  #assertForge(identity: PullRequestKey): void {
    assertReference(identity, this.forge);
  }

  async #commentRows(
    endpoint: string,
  ): Promise<readonly Record<string, unknown>[]> {
    const result: Record<string, unknown>[] = [];
    for (let page = 1; page <= MAX_COMMENT_PAGES; page += 1) {
      const separator = endpoint.includes("?") ? "&" : "?";
      const { data, headers } = await this.#http.request(
        "GET",
        `${endpoint}${separator}per_page=100&page=${page}`,
        { headers: this.#headers() },
      );
      result.push(...records("GitHub comments", data));
      const hasNextPage = headers.get("link")
        ?.split(",")
        .some((link) => /;\s*rel="next"/.test(link)) ?? false;
      if (!hasNextPage) return result;
    }
    throw new Error("GitHub comment pagination limit exceeded");
  }

  /**
   * @spec Review activity includes thread resolution and every comment's id and edit time.
   * @why The newest reply does not reflect edits to earlier comments; REST pagination covers all threads.
   * @case id=review_comment_history,desc=`Edit an older comment on another page`,expect=`Activity key changes`
   * @see {@link ../../tests/forge.test.ts}
   */
  async #reviewThreads(
    identity: PullRequestKey,
  ): Promise<{
    readonly readiness: PullRequestMergeReadiness;
    readonly state: PullRequestReviewThreads;
    readonly activityKey?: string;
  }> {
    const [owner, name] = identity.repository.path.split("/");
    let cursor: string | undefined;
    const activityTokens: string[] = [];
    let unresolved = false;
    for (let page = 0; page < MAX_REVIEW_THREAD_PAGES; page += 1) {
      const { data } = await this.#http.request(
        "POST",
        this.#graphqlUrl,
        {
          headers: this.#headers(),
          body: JSON.stringify({
            query: `query($owner:String!,$name:String!,$number:Int!,$after:String){
              repository(owner:$owner,name:$name){
                pullRequest(number:$number){
                  isDraft
                  mergeStateStatus
                  reviewThreads(first:100,after:$after){
                    nodes{
                      id
                      isResolved
                    }
                    pageInfo{hasNextPage endCursor}
                  }
                }
              }
            }`,
            variables: {
              owner,
              name,
              number: identity.number,
              after: cursor ?? null,
            },
          }),
        },
      );
      const root = record("GitHub GraphQL response", data);
      if (Array.isArray(root.errors) && root.errors.length > 0) {
        const detail = JSON.stringify(root.errors);
        if (/rate.?limit/i.test(detail)) {
          throw this.#http.rateLimit(
            "GitHub GraphQL rate limit exceeded",
          );
        }
        throw new Error("GitHub GraphQL returned errors");
      }
      const graph = record("GitHub GraphQL data", root.data);
      const repository = record("GitHub GraphQL repository", graph.repository);
      const pullRequest = record(
        "GitHub GraphQL PullRequest",
        repository.pullRequest,
      );
      const reviewThreads = record(
        "GitHub GraphQL reviewThreads",
        pullRequest.reviewThreads,
      );
      const nodes = records(
        "GitHub GraphQL reviewThreads.nodes",
        reviewThreads.nodes,
      );
      for (const thread of nodes) {
        if (typeof thread.id !== "string" || !thread.id) {
          throw new Error("GitHub review thread has no id");
        }
        if (typeof thread.isResolved !== "boolean") {
          throw new Error("GitHub review thread has no resolution state");
        }
        unresolved ||= thread.isResolved === false;
        activityTokens.push(JSON.stringify([
          thread.id,
          thread.isResolved,
        ]));
      }
      const pageInfo = record(
        "GitHub GraphQL reviewThreads.pageInfo",
        reviewThreads.pageInfo,
      );
      if (pageInfo.hasNextPage !== true) {
        if (activityTokens.length === 0) return { state: "none", readiness: mergeReadiness(pullRequest) };
        const comments = await this.#commentRows(
          `${this.#restBase}/repos/${repositoryPath(identity.repository.path)}/pulls/${identity.number}/comments`,
        );
        for (const comment of comments) {
          if (
            (typeof comment.id !== "string" && typeof comment.id !== "number") ||
            !String(comment.id)
          ) {
            throw new Error("GitHub review comment has no id");
          }
          if (
            comment.updated_at !== undefined &&
            typeof comment.updated_at !== "string"
          ) {
            throw new Error("GitHub review comment updated_at is invalid");
          }
          activityTokens.push(JSON.stringify([
            String(comment.id),
            comment.updated_at ?? null,
          ]));
        }
        return {
          readiness: mergeReadiness(pullRequest),
          state: unresolved ? "unresolved" : "resolved",
          activityKey: reviewActivityKey("github", activityTokens),
        };
      }
      if (typeof pageInfo.endCursor !== "string" || !pageInfo.endCursor) {
        throw new Error("GitHub review thread cursor missing");
      }
      cursor = pageInfo.endCursor;
    }
    throw new Error("GitHub review thread pagination limit exceeded");
  }
}
