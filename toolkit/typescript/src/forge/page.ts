import type { PageQuery } from "./model.js";

interface Position { version: 1; scope: string; page: number; limit: number }

/** @spec Continuations are bound to the operation, endpoint and page size; no caller URL is followed. */
export function pagePosition(scope: string, query: PageQuery): Position {
  let position: Position = { version: 1, scope, page: 1, limit: query.limit ?? 50 };
  if (query.cursor !== undefined) {
    try { position = JSON.parse(query.cursor) as Position; }
    catch { throw new Error("Invalid repository page cursor"); }
    if (!position || position.version !== 1 || position.scope !== scope ||
        (query.limit !== undefined && query.limit !== position.limit)) {
      throw new Error("Repository page cursor does not match this query");
    }
  }
  if (!Number.isSafeInteger(position.page) || position.page < 1 ||
      !Number.isSafeInteger(position.limit) || position.limit < 1 || position.limit > 100) {
    throw new Error("Repository page requires a positive page and limit between 1 and 100");
  }
  return position;
}

export function pageContinuation(position: Position, more: boolean): string | undefined {
  return more ? JSON.stringify({ ...position, page: position.page + 1 }) : undefined;
}
