import { pullRequestId, type PullRequestSnapshot } from "@compforge/harness-common";
import { assertPullRequestKey, pullRequestKey } from "./key.js";
import { scopeKey } from "./query.js";
import { readIssue } from "./issues.js";
import { ForgeCoverageError, type ForgeReader, type ForgeReadIssue, type PullRequestKey,
  type PullRequestScanCursor, type PullRequestScanQuery, type PullRequestScanResult } from "./model.js";

type ScanWindow = { state: "open" | "all"; from: number; to: number };
interface FailedRead { key: PullRequestKey; issue: ForgeReadIssue }
interface Gap { window: ScanWindow; issue: ForgeReadIssue }
interface ScanPass {
  state: "open" | "all";
  end: number;
  windows: ScanWindow[];
  bufferedPage?: { items: readonly PullRequestKey[]; offset: number; issues: readonly ForgeReadIssue[] };
  trackedKeys: readonly PullRequestKey[];
  refreshOffset: number;
}
interface ScanCursor {
  version: 2;
  binding: string;
  initialSince: number;
  openInventoryEnumerated: boolean;
  enumeratedThrough?: number;
  scannedThrough?: number;
  failedReads: FailedRead[];
  retryOffset: number;
  gaps: Gap[];
  pass?: ScanPass;
}
const SECOND = 1000;
const INITIAL_LOOKBACK_MS = 7 * 24 * 60 * 60 * SECOND;
const OVERLAP_MS = 60 * SECOND;
const PAGE_SIZE = 50;
const MAX_DISCOVERY_REQUESTS_PER_BATCH = 4;
const MAX_DISCOVERED_READS_PER_BATCH = 5;
const MAX_TRACKED_READS_PER_BATCH = 5;
const MAX_RETRIES_PER_BATCH = 2;
const MAX_FAILED_READS = 500;
const iso = (time: number) => new Date(time).toISOString();
const finiteTime = (value: unknown): value is number => typeof value === "number" && Number.isSafeInteger(value) && value >= 0;
const validWindow = (value: ScanWindow): boolean => Boolean(value && ["open", "all"].includes(value.state) && finiteTime(value.from) && finiteTime(value.to) && value.from <= value.to);

function decode(query: PullRequestScanQuery, binding: string, now: number, forge: string): ScanCursor {
  if (query.cursor === undefined) {
    const initialSince = query.initialSince === undefined ? Math.max(0, now - INITIAL_LOOKBACK_MS) : Date.parse(query.initialSince);
    if (!finiteTime(initialSince) || initialSince > now) throw new Error("Invalid PullRequest initialSince");
    return { version: 2, binding, initialSince: Math.floor(initialSince / SECOND) * SECOND,
      openInventoryEnumerated: false, failedReads: [], retryOffset: 0, gaps: [] };
  }
  let cursor: ScanCursor;
  try { cursor = JSON.parse(query.cursor) as ScanCursor; }
  catch { throw new Error("Invalid PullRequest scan cursor"); }
  const pass = cursor?.pass;
  if (!cursor || cursor.version !== 2 || cursor.binding !== binding || !finiteTime(cursor.initialSince) ||
      typeof cursor.openInventoryEnumerated !== "boolean" ||
      (cursor.enumeratedThrough !== undefined && !finiteTime(cursor.enumeratedThrough)) ||
      (cursor.scannedThrough !== undefined && !finiteTime(cursor.scannedThrough)) ||
      !Array.isArray(cursor.failedReads) || cursor.failedReads.length > MAX_FAILED_READS ||
      !Number.isSafeInteger(cursor.retryOffset) || cursor.retryOffset < 0 ||
      !Array.isArray(cursor.gaps) || cursor.gaps.length > 2 || cursor.gaps.some(gap => !gap || !validWindow(gap.window) || !gap.issue) ||
      (pass && (!["open", "all"].includes(pass.state) || !finiteTime(pass.end) || !Array.isArray(pass.windows) || !pass.windows.every(validWindow) ||
        !Array.isArray(pass.trackedKeys) || !Number.isSafeInteger(pass.refreshOffset) || pass.refreshOffset < 0 || pass.refreshOffset > pass.trackedKeys.length ||
        (pass.bufferedPage && (!pass.windows.length || !Array.isArray(pass.bufferedPage.items) || pass.bufferedPage.items.length > PAGE_SIZE ||
          !Number.isSafeInteger(pass.bufferedPage.offset) || pass.bufferedPage.offset < 0 || pass.bufferedPage.offset > pass.bufferedPage.items.length || !Array.isArray(pass.bufferedPage.issues)))))) {
    throw new Error("PullRequest scan cursor version, scope or connection is invalid; start a new scan with initialSince and tracked keys");
  }
  for (const failure of cursor.failedReads) {
    if (!failure?.issue) throw new Error("Invalid failed read in scan cursor");
    assertPullRequestKey(failure.key, forge);
  }
  for (const key of [...(pass?.trackedKeys ?? []), ...(pass?.bufferedPage?.items ?? [])]) assertPullRequestKey(key, forge);
  return cursor;
}

/**
 * @spec Failed detail reads remain retryable without blocking other discovered objects. Missing objects never imply a terminal lifecycle.
 * @spec Enumeration progress and coverage are independent; failed windows and keys survive cursor serialization.
 * @spec Commit the returned cursor only after durable consumption of every item. Replaying a cursor can repeat facts.
 * @why A numbered page in a changing result set cannot prove coverage; exhaustive time windows avoid offset drift.
 * @see {@link component://docs/operations.md#typescript-forge-读取}
 * @case tests/forge-scan.test.mjs
 */
export async function scanPullRequests(reader: Pick<ForgeReader, "forge" | "listPullRequests" | "getPullRequest">, query: PullRequestScanQuery, date: Date,
  withinDeadline: <T>(operation: () => Promise<T>) => Promise<T>, connection: string): Promise<PullRequestScanResult> {
  const now = Math.floor(date.getTime() / SECOND) * SECOND;
  if (!finiteTime(now)) throw new Error("Invalid PullRequest scan clock");
  const binding = JSON.stringify([connection, scopeKey(query.scope, reader.forge)]);
  const cursor = decode(query, binding, now, reader.forge);
  const normalize = (keys: readonly PullRequestKey[]) => [...new Map(keys.map(key => {
    assertPullRequestKey(key, reader.forge);
    if (query.scope.kind === "repository" && key.repository.path !== query.scope.repository.path) throw new Error("Tracked PullRequest is outside scan repository");
    return [pullRequestId(key), pullRequestKey(reader.forge, key.repository.path, key.number)] as const;
  })).values()];
  const trackedKeys = normalize(query.tracked ?? []);
  if (!cursor.pass) {
    const state = cursor.openInventoryEnumerated ? "all" : "open";
    const from = state === "open" ? 0 : cursor.enumeratedThrough === undefined ? cursor.initialSince : Math.max(0, cursor.enumeratedThrough - OVERLAP_MS);
    if (from > now) throw new Error("PullRequest scan clock precedes its cursor");
    cursor.pass = { state, end: now, windows: [...cursor.gaps.map(gap => gap.window), { state, from, to: now }], trackedKeys, refreshOffset: 0 };
    cursor.gaps = [];
  }
  const pass = cursor.pass;
  const items = new Map<string, PullRequestSnapshot>();
  const attempted = new Set<string>();
  const batchIssues: ForgeReadIssue[] = [];
  const failures = new Map(cursor.failedReads.map(failure => [pullRequestId(failure.key), failure]));
  const read = async (key: PullRequestKey, stage: ForgeReadIssue["stage"]): Promise<void> => {
    const id = pullRequestId(key);
    if (attempted.has(id)) return;
    attempted.add(id);
    try {
      const snapshot = await reader.getPullRequest(key);
      if (pullRequestId(snapshot) !== id) throw new Error("Forge returned another PullRequest identity");
      items.set(id, snapshot);
      failures.delete(id);
    } catch (error) {
      const issue = readIssue(error, stage, { key });
      failures.set(id, { key, issue });
      batchIssues.push(issue);
    }
  };
  const retainGap = (window: ScanWindow, issue: ForgeReadIssue) => {
    const previous = cursor.gaps.find(gap => gap.window.state === window.state);
    const retained = previous ? { state: window.state, from: Math.min(previous.window.from, window.from), to: Math.max(previous.window.to, window.to) } : window;
    const gap = { window: retained, issue: { ...issue, scope: query.scope,
      window: { state: retained.state, updatedAfter: iso(retained.from), updatedBefore: iso(retained.to) } } };
    cursor.gaps = [...cursor.gaps.filter(value => value.window.state !== window.state), gap];
  };

  // Rotate persisted failures before adding new ones; a permanently missing key gets one bounded attempt per pass through the queue.
  await withinDeadline(async () => {
    const pending = cursor.failedReads;
    for (let i = 0; i < Math.min(MAX_RETRIES_PER_BATCH, pending.length); i++) {
      await read(pending[(cursor.retryOffset + i) % pending.length]!.key, "retry");
    }
    cursor.retryOffset += Math.min(MAX_RETRIES_PER_BATCH, pending.length);
  });

  await withinDeadline(async () => {
    let requests = 0, reads = 0;
    while (pass.windows.length && reads < MAX_DISCOVERED_READS_PER_BATCH) {
      const window = pass.windows[0]!;
      if (!pass.bufferedPage) {
        if (requests++ >= MAX_DISCOVERY_REQUESTS_PER_BATCH) break;
        try {
          let split = false;
          try {
            const page = await reader.listPullRequests({ scope: query.scope, state: window.state, updatedAfter: iso(window.from), updatedBefore: iso(window.to), pageSize: PAGE_SIZE });
            split = page.nextCursor !== undefined || page.items.length >= PAGE_SIZE;
            if (!split) pass.bufferedPage = { items: page.items, offset: 0, issues: page.issues ?? [] };
          } catch (error) {
            if (!(error instanceof ForgeCoverageError)) throw error;
            split = true;
          }
          if (split) {
            if (window.to - window.from <= SECOND) throw new ForgeCoverageError(`Cannot prove PullRequest coverage within ${iso(window.from)}..${iso(window.to)}`);
            const middle = Math.floor((window.from + window.to) / (2 * SECOND)) * SECOND;
            pass.windows.splice(0, 1, { ...window, to: middle }, { ...window, from: middle });
            continue;
          }
        } catch (error) {
          retainGap(window, readIssue(error, "list", { scope: query.scope }));
          pass.windows.shift();
          continue;
        }
      }
      const page = pass.bufferedPage!;
      while (page.offset < page.items.length && reads < MAX_DISCOVERED_READS_PER_BATCH) {
        const key = page.items[page.offset]!;
        if (failures.size >= MAX_FAILED_READS && !failures.has(pullRequestId(key))) break;
        await read(key, "get");
        page.offset++;
        reads++;
      }
      if (page.offset < page.items.length) break;
      for (const issue of page.issues) retainGap(window, issue);
      pass.bufferedPage = undefined;
      pass.windows.shift();
    }
  });

  // Refresh rounds remain independent of discovery and retry backlogs.
  if (pass.refreshOffset === pass.trackedKeys.length) { pass.trackedKeys = trackedKeys; pass.refreshOffset = 0; }
  await withinDeadline(async () => {
    const end = Math.min(pass.trackedKeys.length, pass.refreshOffset + MAX_TRACKED_READS_PER_BATCH);
    while (pass.refreshOffset < end) {
      const key = pass.trackedKeys[pass.refreshOffset]!;
      if (failures.size >= MAX_FAILED_READS && !failures.has(pullRequestId(key))) break;
      await read(key, "refresh");
      pass.refreshOffset++;
    }
  });
  cursor.failedReads = [...failures.values()];
  cursor.retryOffset %= Math.max(1, cursor.failedReads.length);
  if (!pass.windows.length && pass.refreshOffset === pass.trackedKeys.length) {
    if (pass.state === "open") cursor.openInventoryEnumerated = true;
    else cursor.enumeratedThrough = pass.end;
    cursor.pass = undefined;
    if (!cursor.gaps.length && !cursor.failedReads.length && cursor.enumeratedThrough !== undefined) cursor.scannedThrough = cursor.enumeratedThrough;
  }
  const issues = [...new Map([...cursor.failedReads.map(f => f.issue), ...cursor.gaps.map(g => g.issue), ...batchIssues]
    .map(issue => [JSON.stringify(issue), issue])).values()];
  if (failures.size >= MAX_FAILED_READS) issues.push({ stage: "get", kind: "coverage", message: "PullRequest retry backlog is full; enumeration paused until failed reads recover", scope: query.scope });
  const pending = Boolean(cursor.pass) || cursor.enumeratedThrough === undefined;
  return { items: [...items.values()], cursor: JSON.stringify(cursor) as PullRequestScanCursor, pending,
    complete: !pending && issues.length === 0, issues };
}

/**
 * @spec External providers supply bounded, exact-filtered pages and detail reads; the shared scanner owns coverage and retries.
 * @spec Connection identity binds cursors to one credential/configuration revision. Callers durably consume items before saving the cursor.
 * @link docs/operations.md
 */
export function createPullRequestScanner(reader: Pick<ForgeReader, "forge" | "listPullRequests" | "getPullRequest">,
  options: { readonly connection: string; readonly now?: () => Date; readonly withinDeadline?: <T>(operation: () => Promise<T>) => Promise<T> }) {
  if (!options.connection.trim()) throw new Error("PullRequest scanner connection identity must not be empty");
  return (query: PullRequestScanQuery): Promise<PullRequestScanResult> => scanPullRequests(reader, query,
    (options.now ?? (() => new Date()))(), options.withinDeadline ?? (operation => operation()), options.connection);
}
