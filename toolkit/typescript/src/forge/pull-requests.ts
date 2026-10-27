import { loadingIterator } from "@compforge/ts-stdx/iter";
import { ForgeCoverageError, type ForgeReader, type PullRequestKey, type PullRequestQuery } from "./model.js";

/**
 * @spec A single traversal owns its cursor; errors and page issues reject traversal, never certify a partial inventory.
 * @why Durable scans need scanPullRequests, including explicit coverage gaps and acceptance before cursor commit.
 * @see {@link component://docs/operations.md}
 */
export function iteratePullRequests(reader: Pick<ForgeReader, "listPullRequests">, query: PullRequestQuery): AsyncGenerator<PullRequestKey, void, unknown> {
  const frozen = structuredClone(query);
  let cursor = frozen.cursor;
  return loadingIterator(async () => {
    const page = await reader.listPullRequests({ ...frozen, cursor });
    if (page.issues?.length) throw new ForgeCoverageError(page.issues.map(issue => issue.message).join("; "));
    cursor = page.nextCursor;
    return { items: page.items, hasMore: cursor !== undefined };
  });
}
