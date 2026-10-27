import assert from "node:assert/strict";
import { test } from "node:test";
import { createForgeReader, iterateRepositories } from "../dist/index.js";

function fixture(provider, pages) {
  const calls = [];
  const reader = createForgeReader({ forge: "work", host: provider === "github" ? "github.com" : "gitlab.example.com", provider, token: "fixture" }, {
    fetch: async (url, init) => {
      calls.push({ url: new URL(String(url)), headers: new Headers(init.headers) });
      const response = pages.shift();
      return Response.json(response.body, { status: response.status ?? 200, headers: response.headers });
    },
  });
  return { reader, calls };
}
const row = (provider, path) => provider === "github" ? { full_name: path } : { path_with_namespace: path };

for (const provider of ["github", "gitlab"]) {
  test(`${provider} repository pages keep account scope, common identity and one-request progress`, async () => {
    const { reader, calls } = fixture(provider, [
      { body: [row(provider, "org/one"), row(provider, "org/two")] },
      { body: [row(provider, "other/three")] },
    ]);
    const first = await reader.listRepositories({ limit: 2 });
    assert.equal(calls.length, 1);
    assert.equal(calls[0].url.pathname, provider === "github" ? "/user/repos" : "/api/v4/projects");
    assert.equal(calls[0].url.searchParams.get("per_page"), "2");
    if (provider === "gitlab") assert.equal(calls[0].url.searchParams.get("membership"), "true");
    assert.equal(calls[0].headers.get(provider === "github" ? "authorization" : "private-token"), provider === "github" ? "Bearer fixture" : "fixture");
    assert.deepEqual(JSON.parse(JSON.stringify(first.items)), [
      { forge: { name: "work" }, path: "org/one" }, { forge: { name: "work" }, path: "org/two" },
    ]);
    assert.equal(typeof first.nextCursor, "string");
    const second = await reader.listRepositories({ cursor: first.nextCursor });
    assert.equal(calls.length, 2);
    assert.equal(calls[1].url.searchParams.get("page"), "2");
    assert.equal(calls[1].url.searchParams.get("per_page"), "2");
    assert.deepEqual(second.items.map(item => item.path), ["other/three"]);
    assert.equal(second.nextCursor, undefined);
  });

  test(`${provider} a full page can lead to an empty final page; bad continuations never send requests`, async () => {
    const { reader, calls } = fixture(provider, [{ body: [row(provider, "org/one")] }, { body: [] }]);
    const first = await reader.listRepositories({ limit: 1 });
    assert.ok(first.nextCursor);
    for (const query of [{ limit: 0 }, { limit: 101 }, { cursor: "invalid" }, { cursor: "null" }, { cursor: first.nextCursor, limit: 2 }]) {
      await assert.rejects(reader.listRepositories(query));
    }
    const foreign = createForgeReader({ forge: "another", host: "another.example.com", provider, token: "fixture" }, {
      fetch: async () => { throw new Error("must not send a request"); },
    });
    await assert.rejects(foreign.listRepositories({ cursor: first.nextCursor }), /cursor/);
    assert.equal(calls.length, 1);
    assert.deepEqual(await reader.listRepositories({ cursor: first.nextCursor }), { items: [] });
  });

  test(`${provider} failed or malformed pages do not establish an empty inventory`, async () => {
    for (const response of [{ body: { message: "failed" }, status: 500 }, { body: {} }, { body: [{}] }, { body: [row(provider, "org/../repo")] }]) {
      const { reader } = fixture(provider, [response]);
      await assert.rejects(reader.listRepositories());
    }
  });
}

test("pagination links cannot redirect the credential-bearing repository request", async () => {
  const { reader, calls } = fixture("github", [
    { body: [row("github", "org/one")], headers: { link: '<https://untrusted.invalid/private?page=2>; rel="next"' } },
    { body: [] },
  ]);
  const first = await reader.listRepositories();
  await reader.listRepositories({ cursor: first.nextCursor });
  assert.equal(calls[1].url.origin, "https://api.github.com");
  assert.equal(calls[1].url.pathname, "/user/repos");
  assert.equal(calls[1].url.searchParams.get("page"), "2");
});

for (const provider of ["github", "gitlab"]) {
  test(`${provider} repository iteration loads on demand and includes the final page`, async () => {
    const { reader, calls } = fixture(provider, [
      { body: [row(provider, "org/one"), row(provider, "org/two")] },
      { body: [row(provider, "org/three")] },
    ]);
    const iterator = iterateRepositories(reader, { limit: 2 });
    assert.equal(calls.length, 0);
    assert.equal((await iterator.next()).value.path, "org/one");
    assert.equal((await iterator.next()).value.path, "org/two");
    assert.equal(calls.length, 1);
    assert.deepEqual(await iterator.next(), { value: { forge: { name: "work" }, path: "org/three" }, done: false });
    assert.equal((await iterator.next()).done, true);
    assert.equal(calls.length, 2);
    assert.equal(calls[1].url.searchParams.get("page"), "2");
    assert.equal(calls[1].url.searchParams.get("per_page"), "2");
  });

  test(`${provider} breaking repository iteration never loads another page`, async () => {
    const { reader, calls } = fixture(provider, [{ body: [row(provider, "org/one")] }]);
    const iterator = iterateRepositories(reader, { limit: 1 });
    for await (const repository of iterator) {
      assert.equal(repository.path, "org/one");
      break;
    }
    assert.equal(calls.length, 1);
    assert.equal((await iterator.next()).done, true);
  });

  test(`${provider} repository iteration resumes from a supplied cursor and accepts an empty final page`, async () => {
    const { reader, calls } = fixture(provider, [
      { body: [row(provider, "org/one")] },
      { body: [row(provider, "org/two")] },
      { body: [] },
    ]);
    const first = await reader.listRepositories({ limit: 1 });
    const items = [];
    for await (const repository of iterateRepositories(reader, { cursor: first.nextCursor })) items.push(repository.path);
    assert.deepEqual(items, ["org/two"]);
    assert.equal(calls.length, 3);
    assert.equal(calls[1].url.searchParams.get("per_page"), "1");
  });

  test(`${provider} a later page failure rejects traversal instead of reporting completion`, async () => {
    const { reader, calls } = fixture(provider, [
      { body: [row(provider, "org/one")] },
      { body: { message: "failed" }, status: 500 },
    ]);
    const iterator = iterateRepositories(reader, { limit: 1 });
    assert.equal((await iterator.next()).value.path, "org/one");
    await assert.rejects(iterator.next());
    assert.equal((await iterator.next()).done, true);
    assert.equal(calls.length, 2);
  });
}

test("repository iterators keep independent query state and pass opaque cursors unchanged", async () => {
  const calls = [];
  const reader = { async listRepositories(query) {
    calls.push(query);
    return query.cursor === undefined
      ? { items: [], nextCursor: "opaque-continuation" }
      : { items: [{ forge: { name: "work" }, path: "org/one" }] };
  } };
  const query = { limit: 2 };
  const first = iterateRepositories(reader, query);
  const second = iterateRepositories(reader, query);
  query.limit = 9;
  assert.equal((await first.next()).value.path, "org/one");
  assert.equal((await second.next()).value.path, "org/one");
  assert.deepEqual(calls, [
    { limit: 2, cursor: undefined }, { limit: 2, cursor: "opaque-continuation" },
    { limit: 2, cursor: undefined }, { limit: 2, cursor: "opaque-continuation" },
  ]);
  assert.equal((await first.next()).done, true);
  assert.equal((await second.next()).done, true);
});
