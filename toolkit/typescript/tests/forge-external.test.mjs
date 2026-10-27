import { test } from "node:test";
import assert from "node:assert/strict";
import { createPullRequestScanner } from "../dist/index.js";

test("external read ports reuse the scanner without selecting a built-in provider", async () => {
  const repository = { forge: { name: "forge.example" }, path: "team/repo" };
  const key = { repository, number: 1 };
  const reader = { forge: "forge.example", async listPullRequests(query) { return { items: query.state === "open" ? [] : [key] }; },
    async getPullRequest() { return { ...key, state: "merged", mergedAt: "2026-10-27T00:00:00Z" }; } };
  const scan = createPullRequestScanner(reader, { connection: "access-revision-1", now: () => new Date("2026-10-27T00:00:01Z") });
  let result = await scan({ scope: { kind: "repository", repository } });
  const items = [...result.items];
  for (let i = 0; result.pending && i < 10; i++) { result = await scan({ scope: { kind: "repository", repository }, cursor: result.cursor }); items.push(...result.items); }
  assert(items.some(item => item.state === "merged")); assert.equal(result.complete, true);
  const other = createPullRequestScanner(reader, { connection: "access-revision-2" });
  await assert.rejects(other({ scope: { kind: "repository", repository }, cursor: result.cursor }), /connection is invalid/);
});

test("shared HTTP isolates cancellation and cancels all work on close", async () => {
  const { ForgeHttpClient } = await import("../dist/index.js");
  const http = new ForgeHttpClient({ fetch: async (_url, init) => {
    await new Promise((resolve, reject) => {
      const timer = setTimeout(resolve, 25);
      init.signal.addEventListener("abort", () => { clearTimeout(timer); reject(new Error("aborted")); }, { once: true });
    });
    return Response.json({ ok: true });
  } });
  const controller = new AbortController();
  const first = http.withSignal(controller.signal, () => http.request("GET", "https://forge.example/cancel"));
  const second = http.request("GET", "https://forge.example/continue");
  controller.abort();
  const result = await Promise.allSettled([first, second]);
  assert.equal(result[0].status, "rejected"); assert.equal(result[1].status, "fulfilled");
  http.close(); await assert.rejects(http.request("GET", "https://forge.example/closed"), /cancelled/);
});

test("shared HTTP paces calls and retains cooldown without leaking bodies or query strings", async () => {
  const { ForgeHttpClient } = await import("../dist/index.js");
  const starts = [];
  const http = new ForgeHttpClient({ minIntervalMs: 30, fetch: async () => { starts.push(Date.now()); return Response.json({ ok: true }); } });
  await Promise.all([http.request("GET", "https://forge.example/first"), http.request("GET", "https://forge.example/second")]);
  assert(starts[1] - starts[0] >= 20);
  let calls = 0;
  const limited = new ForgeHttpClient({ fetch: async () => { calls++; return new Response("PRIVATE_BODY", { status: 429, headers: { "retry-after": "60" } }); } });
  for (let i = 0; i < 2; i++) await assert.rejects(limited.request("GET", "https://forge.example/?token=PRIVATE_QUERY"), error => {
    assert(!error.message.includes("PRIVATE")); assert(error.retryAfterMs > 0); return true;
  });
  assert.equal(calls, 1);
});

test("rate limit classification works across independently installed copies", async () => {
  const { isForgeRateLimitError } = await import("../dist/index.js");
  const external = Object.assign(new Error("Rate limited"), { name: "ForgeRateLimitError", retryAfterMs: 1000 });
  assert(isForgeRateLimitError(external)); assert(!isForgeRateLimitError({ name: "ForgeRateLimitError", retryAfterMs: 1000 }));
  assert(!isForgeRateLimitError(Object.assign(new Error(), { name: "ForgeRateLimitError", retryAfterMs: NaN })));
});
