import { createHash } from "node:crypto";
import type { PullRequestQuery, PullRequestScope } from "./model.js";
import { assertPullRequestKey } from "./key.js";

/** Cursor identity binds connection credentials without persisting secrets. */
export function connectionKey(provider: string, forge: string, endpoint: string, token: string): string {
  return createHash("sha256").update(JSON.stringify([provider, forge, endpoint, token])).digest("hex");
}
export function scopeKey(scope: PullRequestScope, forge: string): string {
  if (!scope || (scope.kind !== "account" && scope.kind !== "repository")) throw new Error("Invalid PullRequest query scope");
  if (scope.kind === "repository") assertPullRequestKey({ repository: scope.repository, number: 1 }, forge);
  if ((scope.kind === "account" || scope.author !== undefined) && !/^[a-zA-Z0-9_.-]+$/.test(scope.author ?? "")) {
    throw new Error("PullRequest author requires a platform username");
  }
  return JSON.stringify([scope.kind, scope.kind === "repository" ? scope.repository.path : null, scope.author?.toLowerCase() ?? null]);
}
export function queryKey(query: PullRequestQuery, forge: string): string {
  const scope = scopeKey(query.scope, forge);
  if (!["all", "open", "closed", "merged"].includes(query.state ?? "all")) throw new Error("Invalid PullRequest query state");
  const start = query.updatedAfter === undefined ? undefined : Date.parse(query.updatedAfter);
  const end = query.updatedBefore === undefined ? undefined : Date.parse(query.updatedBefore);
  if ((start !== undefined && !Number.isFinite(start)) || (end !== undefined && !Number.isFinite(end)) ||
      (start !== undefined && end !== undefined && start > end)) throw new Error("Invalid PullRequest time window");
  return JSON.stringify([scope, query.state ?? "all", start ?? null, end ?? null]);
}
export function authorMatches(value: unknown, field: "login" | "username", expected?: string): boolean {
  if (expected === undefined) return true;
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const actual = (value as Record<string, unknown>)[field];
  return typeof actual === "string" && actual.toLowerCase() === expected.toLowerCase();
}
export function nextPage(headers: Headers, page: number, provider: "github" | "gitlab"): number | undefined {
  if (provider === "github") return headers.get("link")?.split(",").some(link => /;\s*rel="next"/.test(link)) ? page + 1 : undefined;
  const next = headers.get("x-next-page");
  if (!next) return undefined;
  if (!/^\d+$/.test(next) || !Number.isSafeInteger(Number(next)) || Number(next) <= page) throw new Error("Forge pagination did not advance");
  return Number(next);
}
