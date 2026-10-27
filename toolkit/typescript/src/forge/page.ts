import type { PageQuery } from "./model.js";
interface Position { version: 2; scope: string; page: number; pageSize: number }

/** @spec Continuations bind query, connection and page size; no caller URL is followed. */
export function pagePosition(scope: string, query: PageQuery): Position {
  let position: Position = { version: 2, scope, page: 1, pageSize: query.pageSize ?? 50 };
  if (query.cursor !== undefined) {
    try { position = JSON.parse(query.cursor) as Position; }
    catch { throw new Error("Invalid page cursor"); }
    if (!position || position.version !== 2 || position.scope !== scope ||
        (query.pageSize !== undefined && query.pageSize !== position.pageSize)) throw new Error("Page cursor does not match this query or connection");
  }
  if (!Number.isSafeInteger(position.page) || position.page < 1 ||
      !Number.isSafeInteger(position.pageSize) || position.pageSize < 1 || position.pageSize > 100) {
    throw new Error("Page requires a positive page and pageSize between 1 and 100");
  }
  return position;
}
export function pageContinuation(position: Position, next: number | undefined): string | undefined {
  return next === undefined ? undefined : JSON.stringify({ ...position, page: next });
}
