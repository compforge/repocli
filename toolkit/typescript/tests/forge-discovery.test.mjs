import assert from "node:assert/strict";
import { test } from "node:test";
import { GitHubForgeReader, GitLabForgeReader } from "../dist/index.js";

const wire = value => JSON.parse(JSON.stringify(value));
function assertFields(actual, expected) {
  assert.deepEqual(Object.fromEntries(Object.keys(expected).map(key => [key, actual[key]])), expected);
}


const json = (data, headers = {}) => new Response(JSON.stringify(data), { headers });
const before = "2026-10-26T00:00:00.000Z";
test("GitHub author discovery spans target repositories and refuses incomplete search coverage", async () => {
    let incomplete = false, total = 51;
    const urls = [];
    const fetch = async (input) => {
        urls.push(new URL(String(input)));
        return json({ incomplete_results: incomplete, total_count: total,
            items: [1, 2].map(n => ({ number: 7, user: { login: "me" }, pull_request: {}, repository_url: `https://api.github.com/repos/org/repo${n}` })) }, { link: '<https://api.github.com/search/issues?page=2>; rel="next"' });
    };
    const connector = new GitHubForgeReader({ forge: "github.com", host: "github.com", provider: "github", token: "fixture" }, { fetch });
    const query = { author: "me", state: "all", updatedAfter: "2026-10-25T00:00:00.000Z", updatedBefore: before };
    const page = await connector.discover(query);
    assert.deepEqual(wire(page.items.map(i => i.repository.path)), wire(["org/repo1", "org/repo2"]));
    assert.equal(page.nextCursor, "2");
    assert.equal(urls[0].searchParams.get("q"), `is:pr author:me updated:${query.updatedAfter}..${before}`);
    await connector.discover({ ...query, state: "open", cursor: "2" });
    assert.ok(urls[1].searchParams.get("q").includes("is:open"));
    incomplete = true;
    await assert.rejects(connector.discover(query), new RegExp("incomplete"));
    incomplete = false;
    total = 1001;
    await assert.rejects(connector.discover(query), new RegExp("1000"));
    const calls = urls.length;
    await assert.rejects(connector.discover({ ...query, author: "me repo:secret" }), new RegExp("username"));
    assert.equal(urls.length, calls);
});
test("GitLab uses explicit author across projects, including merged MRs and nested target paths", async () => {
    const urls = [];
    const fetch = async (input) => {
        urls.push(new URL(String(input)));
        return json([
            { iid: 8, state: "merged", author: { username: "me" }, web_url: "https://gitlab.test/team/sub/repo/-/merge_requests/8" },
            { iid: 8, state: "opened", author: { username: "me" }, web_url: "https://gitlab.test/another/repo/-/merge_requests/8" },
        ], { "x-next-page": "2" });
    };
    const connector = new GitLabForgeReader({ forge: "work", host: "gitlab.test", provider: "gitlab", token: "fixture" }, { fetch });
    const page = await connector.discover({ author: "me", state: "all", updatedBefore: before });
    assert.equal(urls[0].pathname, "/api/v4/merge_requests");
    assert.equal(urls[0].searchParams.get("scope"), "all");
    assert.equal(urls[0].searchParams.get("author_username"), "me");
    assert.equal(urls[0].searchParams.get("state"), "all");
    assert.deepEqual(wire(page), wire({ items: [{ repository: { forge: { name: "work" }, path: "team/sub/repo" }, number: 8 }, { repository: { forge: { name: "work" }, path: "another/repo" }, number: 8 }], nextCursor: "2" }));
});
test("GitHub and GitLab retain the head/base and terminal facts required by a Source", async () => {
    const github = new GitHubForgeReader({ forge: "github.com", host: "github.com", provider: "github", token: "fixture" }, { fetch: async (input) => {
            if (String(input).endsWith("/graphql"))
                return json({ errors: [{ message: "unavailable" }] });
            return json({ number: 1, title: "PR", html_url: "https://github.com/org/repo/pull/1", state: "closed", merged_at: before, updated_at: before,
                head: { sha: "head" }, base: { sha: "base" } });
        } });
    const gitlab = new GitLabForgeReader({ forge: "work", host: "gitlab.test", provider: "gitlab", token: "fixture" }, { fetch: async (input) => {
            if (String(input).includes("discussions"))
                return json([]);
            return json({ iid: 1, title: "MR", web_url: "https://gitlab.test/org/repo/-/merge_requests/1", state: "merged", merged_at: before, updated_at: before,
                sha: "head", diff_refs: { base_sha: "base" } });
        } });
    for (const connector of [github, gitlab])
        assertFields(await connector.get({ repository: { forge: { name: connector.forge }, path: "org/repo" }, number: 1 }), {
            state: "merged", headSha: "head", baseSha: "base", updatedAt: before, mergedAt: before,
        });
});
export {};
