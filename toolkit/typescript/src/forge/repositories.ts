import type { RepositorySnapshot } from "@compforge/harness-common";
import { loadingIterator } from "@compforge/ts-stdx/iter";
import type { ForgeReader, PageQuery } from "./model.js";

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
  const { limit } = query;
  let cursor = query.cursor;
  return loadingIterator(async () => {
    const page = await reader.listRepositories({ limit, cursor });
    cursor = page.nextCursor;
    return { items: page.items, hasMore: cursor !== undefined };
  });
}
