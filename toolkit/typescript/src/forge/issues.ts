import { ForgeAuthError, ForgeCoverageError, ForgeError, ForgeNotFound, ForgeRateLimitError, type ForgeReadIssue } from "./model.js";
export function readIssue(error: unknown, stage: ForgeReadIssue["stage"], subject: Pick<ForgeReadIssue, "key" | "scope" | "window"> = {}): ForgeReadIssue {
  return { ...subject, stage, message: error instanceof Error ? error.message : String(error),
    kind: error instanceof ForgeRateLimitError ? "rate_limit" : error instanceof ForgeAuthError ? "auth"
      : error instanceof ForgeNotFound ? "not_found" : error instanceof ForgeCoverageError ? "coverage"
      : error instanceof ForgeError ? "transport" : "invalid_data",
    ...(error instanceof ForgeRateLimitError ? { retryAfterMs: error.retryAfterMs } : {}) };
}
