import type { RepositorySnapshot } from "@compforge/harness-common";
import { loadingIterator } from "@compforge/ts-stdx/iter";
import { ForgeCoverageError, type ForgeReader, type PageQuery } from "./model.js";

/**
 * Traverse repository pages with one private cursor per iterator.
 * @spec Loading is lazy and sequential; early exit stops pagination and page errors reject iteration.
 * @why A traversal cursor tracks fetched pages, not durable consumer progress. Persisted scans use listRepositories.
 * @see {@link component://docs/operations.md#forge-仓库分页}
 * @case tests/forge-repositories.test.mjs
 */
export function iterateRepositories(
  reader: Pick<ForgeReader, "listRepositories">,
  query: PageQuery = {},
): AsyncGenerator<RepositorySnapshot, void, unknown> {
  const { pageSize } = query;
  let cursor = query.cursor;
  return loadingIterator(async () => {
    const page = await reader.listRepositories({ pageSize, cursor });
    if (page.issues?.length) throw new ForgeCoverageError(page.issues.map(issue => issue.message).join("; "));
    cursor = page.nextCursor;
    return { items: page.items, hasMore: cursor !== undefined };
  });
}
