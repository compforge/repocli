import assert from "node:assert/strict";
import test from "node:test";
import { createServer } from "node:http";
import { once } from "node:events";
import { pullRequestId, repositoryId } from "@compforge/harness-common";
import {
  createForgeReader, ForgeError, ForgeAuthError, ForgeNotFound,
} from "../dist/index.js";

const config = { forge: "github", provider: "github", host: "github.com", token: "fixture" };
const repository = { forge: { name: "github" }, path: "example/project", id() { return repositoryId(this); } };
const reference = { repository, number: 7 };
const json = data => new Response(JSON.stringify(data));

test("common PullRequest facts retain target identity across fork commits and merge", async () => {
  let state = "open", sha = "old";
  const reader = createForgeReader(config, { fetch: async input => {
    if (String(input).endsWith("/graphql")) return new Response("review access denied", {status: 403});
    return json({number: 7, title: "Improve parser", html_url: "https://github.com/example/project/pull/7",
      state, merged: state === "closed", head: {sha, ref: "parser", repo: {full_name: "contributor/project"}},
      base: {sha: "base", ref: "main"}});
  } });
  const before = await reader.get(reference);
  state = "closed"; sha = "new";
  const merged = await reader.get(reference);
  assert.equal(pullRequestId(before), "github/example/project#7");
  assert.equal(pullRequestId(merged), pullRequestId(before));
  assert.equal(merged.state, "merged"); assert.equal(merged.headSha, "new");
  assert.equal(repositoryId(merged.repository), repository.id());
  assert.equal(repositoryId(merged.sourceRepository), "github/contributor/project");
  assert.equal(merged.sourceBranch, "parser"); assert.equal(merged.targetBranch, "main");
  assert.equal(merged.reviewThreads, "unknown"); assert.match(merged.reviewError, /403/);
  assert.equal(merged.mergedAt, undefined);
  assert.deepEqual(structuredClone(merged), JSON.parse(JSON.stringify(merged)));
  assert.equal("id" in merged, false);
  assert.equal("id" in merged.repository, false);
  assert.equal("id" in merged.sourceRepository, false);
});

test("GitLab same-project source is explicit; a foreign source remains unknown", async () => {
  let source = 42;
  const reader = createForgeReader({ ...config, provider: "gitlab", host: "gitlab.example.com" }, { fetch: async input => {
    if (String(input).includes("discussions")) return json([]);
    return json({iid: 7, title: "Improve parser", web_url: "https://gitlab.example.com/example/project/-/merge_requests/7",
      state: "opened", source_project_id: source, target_project_id: 42, source_branch: "parser", target_branch: "main"});
  } });
  assert.equal(repositoryId((await reader.get(reference)).sourceRepository), repository.id());
  source = 43;
  const pr = await reader.get(reference);
  assert.equal(pr.sourceRepository, undefined);
  assert.equal(pr.sourceBranch, "parser"); assert.equal(pr.targetBranch, "main");
  source = null;
  assert.equal((await reader.get(reference)).sourceRepository, undefined);
});

test("transport preserves typed auth, missing and invalid-JSON errors", async () => {
  for (const [status, ErrorType] of [[401, ForgeAuthError], [403, ForgeAuthError], [404, ForgeNotFound], [500, ForgeError]]) {
    const reader = createForgeReader(config, { fetch: async () => new Response("unavailable", { status }) });
    await assert.rejects(reader.get(reference), error => error instanceof ErrorType && error.message.includes(String(status)));
  }
  const reader = createForgeReader(config, { fetch: async () => new Response("{") });
  await assert.rejects(reader.get(reference), error => error instanceof ForgeError && /invalid JSON/.test(error.message));
});

test("caller identity and HTTP limits are checked before network access", async () => {
  let calls = 0;
  const reader = createForgeReader(config, { fetch: async () => { calls++; return json({}); } });
  for (const invalid of [ { ...reference, number: 0 }, { ...reference, number: 1.5 },
    { ...reference, repository: {...repository, forge: {name: "other"}} },
    { ...reference, repository: {...repository, path: "example/../project"} } ]) {
    await assert.rejects(reader.get(invalid));
  }
  assert.equal(calls, 0);
  for (const options of [{timeoutMs: 0}, {timeoutMs: Infinity}, {maxResponseBytes: -1}]) {
    assert.throws(() => createForgeReader(config, options));
  }
});

test("streaming bodies over the response limit are cancelled", async () => {
  let cancelled = false;
  const reader = createForgeReader(config, { maxResponseBytes: 8, fetch: async () => new Response(new ReadableStream({
    start(controller) { controller.enqueue(new Uint8Array(16)); },
    cancel() { cancelled = true; },
  })) });
  await assert.rejects(reader.get(reference), /exceeded 8 response bytes/);
  assert.equal(cancelled, true);
});

for (const phase of ["headers", "body"]) test(`the deadline aborts native fetch during ${phase}`, { timeout: 5000 }, async t => {
  const server = createServer((_request, response) => {
    if (phase === "body") { response.writeHead(200, {"content-type": "application/json"}); response.write("{"); }
  });
  t.after(() => { server.closeAllConnections(); server.close(); });
  server.listen(0, "127.0.0.1"); await once(server, "listening");
  const reader = createForgeReader(config, { timeoutMs: 30,
    fetch: (_input, init) => fetch(`http://127.0.0.1:${server.address().port}/slow`, init) });
  await assert.rejects(reader.get(reference), error => error instanceof ForgeError && /abort/i.test(error.message));
});
