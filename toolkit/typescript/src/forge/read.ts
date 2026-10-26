import { pullRequestId } from "@compforge/harness-common";
import { assertReference, reference } from "./identity.js";
import { discoveryPage } from "./discovery.js";
import { ForgeDiscoveryOverflowError, type ForgeReader, type PullRequestCursor, type PullRequestReadQuery, type PullRequestReadResult, type PullRequestKey, type PullRequestObservation } from "./model.js";

type Window = [number, number];
interface Scan {
  state: "open" | "all";
  end: number;
  windows: Window[];
  page?: { items: readonly PullRequestKey[]; offset: number };
}
interface Cursor {
  version: 1;
  forge: string;
  author: string;
  since: number;
  initialized: boolean;
  through?: number;
  cycle?: { scan?: Scan; refresh: readonly PullRequestKey[]; offset: number };
}
const SECOND = 1000;
const LOOKBACK = 7 * 24 * 60 * 60 * SECOND;
const OVERLAP = 60 * SECOND;
const BATCH_SIZE = 5;
const WINDOW_READS = 4;
const iso = (time: number) => new Date(time).toISOString();

function decode(reader: ForgeReader, query: PullRequestReadQuery, now: number): Cursor {
  if (!query.cursor) {
    const since = query.since ? Date.parse(query.since) : now - LOOKBACK;
    if (!Number.isFinite(since) || since < 0 || since > now) throw new Error("Invalid PullRequest read since");
    return { version: 1, forge: reader.forge, author: query.author, since: Math.floor(since / SECOND) * SECOND, initialized: false };
  }
  const cursor = JSON.parse(query.cursor) as Cursor;
  if (cursor.version !== 1 || cursor.forge !== reader.forge || cursor.author !== query.author ||
      !Number.isFinite(cursor.since) || typeof cursor.initialized !== "boolean") {
    throw new Error("PullRequest cursor version or account does not match this reader");
  }
  return cursor;
}

/**
 * @spec Checkpoints are application-owned opaque values; commit the returned cursor only after consuming every item.
 * @spec Discovery consumes only exhaustive single-response windows, never offsets into a mutable result set.
 * @why Updating one earlier result can move an unseen merged PR across a numbered page boundary.
 * @spec An unsplittable or failed window is retained and reported; known PR reads still proceed.
 * @see {@link component://docs/operations.md}
 */
export async function readPullRequests(reader: ForgeReader, query: PullRequestReadQuery, date: Date, withinDeadline: <T>(operation: () => Promise<T>) => Promise<T>): Promise<PullRequestReadResult> {
  const now = Math.floor(date.getTime() / SECOND) * SECOND;
  discoveryPage({ author: query.author, state: "all", updatedBefore: iso(now) });
  for (const key of query.tracked ?? []) assertReference(key, reader.forge);
  const cursor = decode(reader, query, now);
  if (!cursor.cycle) {
    const from = cursor.initialized ? cursor.through === undefined ? cursor.since : Math.max(0, cursor.through - OVERLAP) : 0;
    if (from > now) throw new Error("PullRequest reader clock precedes its cursor");
    cursor.cycle = { scan: { state: cursor.initialized ? "all" : "open", end: now, windows: [[from, now]] },
      refresh: [...new Map((query.tracked ?? []).map(key => [pullRequestId(key), reference(key.repository.forge.name, key.repository.path, key.number)])).values()], offset: 0 };
  }
  const cycle = cursor.cycle;
  const items = new Map<string, PullRequestObservation>();
  const errors: string[] = [];
  const read = async (key: PullRequestKey) => {
    if (items.has(pullRequestId(key))) return;
    const observation = await reader.get(key);
    if (pullRequestId(observation) !== pullRequestId(key)) throw new Error("Forge returned another PullRequest identity");
    items.set(pullRequestId(key), observation);
  };
  const scan = cycle.scan;
  if (scan) try {
    await withinDeadline(async () => {
      let requests = 0;
      while (scan.windows.length && items.size < BATCH_SIZE) {
        if (!scan.page) {
          if (requests++ === WINDOW_READS) break;
          const [from, to] = scan.windows[0]!;
          let split = false;
          try {
            const page = await reader.discover({ author: query.author, state: scan.state, updatedAfter: iso(from), updatedBefore: iso(to) });
            // A full GitLab page without pagination headers is not evidence of exhaustion.
            split = Boolean(page.nextCursor) || page.items.length >= 50;
            if (!split) scan.page = { items: page.items, offset: 0 };
          } catch (error) {
            if (!(error instanceof ForgeDiscoveryOverflowError)) throw error;
            split = true;
          }
          if (split) {
            if (to - from <= SECOND) throw new Error(`PullRequest discovery cannot prove coverage within ${iso(from)}..${iso(to)}; cursor retained`);
            const middle = Math.floor((from + to) / (2 * SECOND)) * SECOND;
            // Inclusive overlap also preserves subsecond GitLab timestamps at partition boundaries.
            scan.windows.splice(0, 1, [from, middle], [middle, to]);
            continue;
          }
        }
        const page = scan.page!;
        while (page.offset < page.items.length && items.size < BATCH_SIZE) {
          await read(page.items[page.offset]!);
          page.offset++;
        }
        if (page.offset < page.items.length) break;
        scan.page = undefined;
        scan.windows.shift();
      }
      if (!scan.windows.length) {
        if (scan.state === "open") cursor.initialized = true;
        else cursor.through = scan.end;
        cycle.scan = undefined;
      }
    });
  } catch (error) {
    errors.push(`PullRequest discovery for ${reader.forge}/${query.author}: ${String(error)}`);
  }

  // Restart refresh rounds even while a discovery window is blocked.
  if (cycle.offset === cycle.refresh.length) {
    cycle.refresh = [...new Map((query.tracked ?? []).map(key => [pullRequestId(key), reference(key.repository.forge.name, key.repository.path, key.number)])).values()];
    cycle.offset = 0;
  }
  // Keep known proposals live even when discovery is unavailable or over capacity.
  await withinDeadline(async () => {
    const end = Math.min(cycle.refresh.length, cycle.offset + BATCH_SIZE);
    while (cycle.offset < end) {
      const key = cycle.refresh[cycle.offset]!;
      try { await read(key); }
      catch (error) { errors.push(`PullRequest ${pullRequestId(key)}: ${String(error)}`); }
      cycle.offset++;
    }
  });
  if (!cycle.scan && cycle.offset === cycle.refresh.length) cursor.cycle = undefined;
  return { items: [...items.values()], cursor: JSON.stringify(cursor) as PullRequestCursor,
    pending: Boolean(cursor.cycle) || cursor.through === undefined, errors };
}
