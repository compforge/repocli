import assert from "node:assert/strict";
import { test, describe } from "node:test";
import { GitHubForgeReader, GitLabForgeReader, ForgeRateLimitError } from "../dist/index.js";

const wire = value => JSON.parse(JSON.stringify(value));
function assertFields(actual, expected) {
  assert.deepEqual(Object.fromEntries(Object.keys(expected).map(key => [key, actual[key]])), expected);
}
function assertObservation(actual, expected) {
  const {reviewActivityKey, ...facts} = wire(actual);
  assert.equal(typeof reviewActivityKey, "string");
  assert.deepEqual(facts, wire(expected));
}

function json(data, headers) {
    return new Response(JSON.stringify(data), {
        status: 200,
        headers: {
            "content-type": "application/json",
            ...headers,
        },
    });
}
describe("GitHubForgeReader", () => {
    test("backs off after a rate-limit response without issuing more requests", async () => {
        let calls = 0;
        const fetch = async () => {
            calls += 1;
            return new Response(JSON.stringify({ message: "API rate limit exceeded" }), {
                status: 403,
                headers: {
                    "content-type": "application/json",
                    "retry-after": "120",
                    "x-ratelimit-remaining": "0",
                },
            });
        };
        const connector = new GitHubForgeReader({
            forge: "github.com",
            provider: "github",
            host: "github.com",
            token: "secret",
        }, { fetch });
        const query = () => connector.list("owner/repo", {
            state: "open",
            limit: 1,
        });
        await assert.rejects(query(), ForgeRateLimitError);
        await assert.rejects(query(), error => {
            assert.ok(error.retryAfterMs > 0);
            return true;
        });
        assert.equal(calls, 1);
    });
    test("propagates GraphQL rate-limit errors instead of hiding them", async () => {
        const fetch = async (input) => {
            if (String(input).endsWith("/pulls/3")) {
                return json({ number: 3, state: "open", mergeable: true });
            }
            return json({ errors: [{ message: "API rate limit exceeded" }] });
        };
        const connector = new GitHubForgeReader({
            forge: "github.com",
            provider: "github",
            host: "github.com",
            token: "secret",
        }, { fetch });
        await assert.rejects(connector.get({ repository: { forge: { name: "github.com" }, path: "owner/repo" }, number: 3 }), ForgeRateLimitError);
    });
    test("maps lifecycle, conflicts, review threads, and discovery", async () => {
        const calls = [];
        const fetch = async (input, init) => {
            const url = String(input);
            calls.push({ url, headers: new Headers(init?.headers) });
            if (url.endsWith("/pulls/17")) {
                return json({
                    number: 17,
                    title: "Keep Board focused",
                    html_url: "https://github.com/compforge/reqloop/pull/17",
                    state: "open",
                    mergeable: false,
                });
            }
            if (url.includes("/pulls/17/comments?")) {
                return json([
                    { id: 1, updated_at: "2026-07-26T07:00:00Z" },
                    { id: 2, updated_at: "2026-07-26T07:30:00Z" },
                ]);
            }
            if (url.endsWith("/graphql")) {
                return json({
                    data: {
                        repository: {
                            pullRequest: {
                                reviewThreads: {
                                    nodes: [
                                        {
                                            id: "PRRT_1",
                                            isResolved: true,
                                            comments: {
                                                nodes: [{
                                                        id: "PRRC_1",
                                                        updatedAt: "2026-07-26T07:00:00Z",
                                                    }],
                                            },
                                        },
                                        {
                                            id: "PRRT_2",
                                            isResolved: false,
                                            comments: {
                                                nodes: [{
                                                        id: "PRRC_2",
                                                        updatedAt: "2026-07-26T07:30:00Z",
                                                    }],
                                            },
                                        },
                                    ],
                                    pageInfo: { hasNextPage: false, endCursor: null },
                                },
                            },
                        },
                    },
                });
            }
            if (url.includes("/pulls?")) {
                const state = new URL(url).searchParams.get("state");
                return state === "open"
                    ? json([{ number: 17, state: "open", merged_at: null }])
                    : json([
                        {
                            number: 16,
                            state: "closed",
                            merged_at: "2026-07-25T08:00:00Z",
                        },
                        { number: 15, state: "closed", merged_at: null },
                    ]);
            }
            return new Response("not found", { status: 404 });
        };
        const connector = new GitHubForgeReader({
            forge: "github.com",
            provider: "github",
            host: "github.com",
            token: "secret",
        }, {
            fetch,
            now: () => new Date("2026-07-26T08:00:00.000Z"),
        });
        assertObservation(await connector.get({ repository: { forge: { name: "github.com" }, path: "compforge/reqloop" }, number: 17 }), {
            ...{ repository: { forge: { name: "github.com" }, path: "compforge/reqloop" }, number: 17 },
            title: "Keep Board focused",
            url: "https://github.com/compforge/reqloop/pull/17",
            state: "open",
            reviewThreads: "unresolved",
            mergeability: "conflicted",
            observedAt: "2026-07-26T08:00:00.000Z"
        });
        assert.deepEqual(wire(await connector.list("compforge/reqloop", {
            state: "open",
            limit: 2,
        })), wire([
            { repository: { forge: { name: "github.com" }, path: "compforge/reqloop" }, number: 17 },
        ]));
        assert.deepEqual(wire(await connector.list("compforge/reqloop", {
            state: "merged",
            limit: 2,
        })), wire([
            { repository: { forge: { name: "github.com" }, path: "compforge/reqloop" }, number: 16 },
        ]));
        assert.equal(calls[0]?.headers.get("authorization"), "Bearer secret");
        assert.ok(calls.map((call) => call.url).includes("https://api.github.com/graphql"));
        assert.equal(calls.some((call) => call.url.includes("/pulls?state=open")), true);
        assert.equal(calls.some((call) => call.url.includes("/pulls?state=closed")), true);
    });
    test("keeps the observation when review thread API is unavailable", async () => {
        const fetch = async (input) => {
            if (String(input).endsWith("/pulls/3")) {
                return json({
                    number: 3,
                    title: "Already merged",
                    html_url: "https://github.com/owner/repo/pull/3",
                    state: "closed",
                    merged: true,
                    mergeable: null,
                });
            }
            return new Response("forbidden", { status: 403 });
        };
        const connector = new GitHubForgeReader({
            forge: "github.com",
            provider: "github",
            host: "github.com",
            token: "secret",
        }, { fetch });
        assertFields(await connector.get({ repository: { forge: { name: "github.com" }, path: "owner/repo" }, number: 3 }), {
            state: "merged",
            reviewThreads: "unknown",
            mergeability: "unknown",
        });
    });
    test("applies the caller's PullRequest state and result limit", async () => {
        const pages = [];
        const fetch = async (input) => {
            const url = new URL(String(input));
            const state = url.searchParams.get("state");
            const page = Number(url.searchParams.get("page"));
            pages.push(`${state}:${page}`);
            if (state === "open" && page === 1) {
                return json([
                    { number: 3, state: "open", merged_at: null },
                ], {
                    link: '<https://api.github.com/repos/owner/repo/pulls?page=2>; rel="next"',
                });
            }
            if (state === "open") {
                return json([{ number: 1, state: "open", merged_at: null }]);
            }
            return json([
                { number: 4, state: "closed", merged_at: null },
                {
                    number: 2,
                    state: "closed",
                    merged_at: "2026-07-25T08:00:00Z",
                },
            ]);
        };
        const connector = new GitHubForgeReader({
            forge: "github.com",
            provider: "github",
            host: "github.com",
            token: "secret",
        }, { fetch });
        assert.deepEqual(wire(await connector.list("owner/repo", {
            state: "open",
            limit: 1,
        })), wire([
            { repository: { forge: { name: "github.com" }, path: "owner/repo" }, number: 3 },
        ]));
        assert.deepEqual(wire(await connector.list("owner/repo", {
            state: "merged",
            limit: 1,
        })), wire([
            { repository: { forge: { name: "github.com" }, path: "owner/repo" }, number: 2 },
        ]));
        assert.deepEqual(wire(pages), wire(["open:1", "closed:1"]));
    });
    test("optionally filters PullRequests by configured author uids", async () => {
        let requestedUrl = "";
        const fetch = async (input) => {
            requestedUrl = String(input);
            return json([
                {
                    number: 3,
                    state: "open",
                    user: { login: "someone-else", id: 7 },
                },
                {
                    number: 2,
                    state: "open",
                    user: { login: "owner", id: 42 },
                },
                {
                    number: 1,
                    state: "open",
                    user: { login: "backup", id: 43 },
                },
            ]);
        };
        const connector = new GitHubForgeReader({
            forge: "github.com",
            provider: "github",
            host: "github.com",
            token: "secret",
            uids: ["owner", "43"],
        }, { fetch });
        assert.deepEqual(wire(await connector.list("owner/repo", {
            state: "open",
            limit: 2,
        })), wire([
            { repository: { forge: { name: "github.com" }, path: "owner/repo" }, number: 2 },
            { repository: { forge: { name: "github.com" }, path: "owner/repo" }, number: 1 },
        ]));
        assert.equal(new URL(requestedUrl).searchParams.get("per_page"), "100");
    });
    test("tracks earlier comment edits across pages and rejects incomplete snapshots", async () => {
        let updatedAt = "2026-07-26T07:00:00Z";
        let resolved = false;
        let deleted = false;
        let failPage = false;
        const pages = [];
        const fetch = async (input) => {
            const url = String(input);
            if (url.endsWith("/pulls/5")) {
                return json({
                    number: 5,
                    title: "Ready to merge",
                    html_url: "https://github.com/owner/repo/pull/5",
                    state: "open",
                    mergeable: true,
                });
            }
            if (url.endsWith("/graphql")) {
                return json({
                    data: {
                        repository: {
                            pullRequest: {
                                reviewThreads: {
                                    nodes: [{
                                            id: "PRRT_1",
                                            isResolved: resolved,
                                            comments: {
                                                nodes: [{
                                                        id: "PRRC_2",
                                                        updatedAt: "2026-07-26T08:00:00Z",
                                                    }],
                                            },
                                        }],
                                    pageInfo: { hasNextPage: false, endCursor: null },
                                },
                            },
                        },
                    },
                });
            }
            if (url.includes("/pulls/5/comments?")) {
                const page = new URL(url).searchParams.get("page");
                pages.push(page);
                if (page === "1") {
                    return json([{ id: 2, updated_at: "2026-07-26T08:00:00Z" }], {
                        link: '<https://api.github.com/repos/owner/repo/pulls/5/comments?per_page=100&page=2>; rel="next"',
                    });
                }
                if (failPage)
                    return new Response("unavailable", { status: 503 });
                return json(deleted ? [] : [{ id: 1, updated_at: updatedAt }]);
            }
            return new Response("not found", { status: 404 });
        };
        const connector = new GitHubForgeReader({
            forge: "github.com",
            provider: "github",
            host: "github.com",
            token: "secret",
        }, { fetch });
        const identity = { repository: { forge: { name: "github.com" }, path: "owner/repo" }, number: 5 };
        const first = await connector.get(identity);
        updatedAt = "2026-07-26T09:00:00Z";
        const second = await connector.get(identity);
        assert.equal(typeof first.reviewActivityKey, "string");
        assert.equal(typeof second.reviewActivityKey, "string");
        assert.notEqual(second.reviewActivityKey, first.reviewActivityKey);
        assert.deepEqual(wire(pages), wire(["1", "2", "1", "2"]));
        assert.equal((await connector.get(identity)).reviewActivityKey, second.reviewActivityKey);
        resolved = true;
        const third = await connector.get(identity);
        assert.equal(third.reviewThreads, "resolved");
        assert.notEqual(third.reviewActivityKey, second.reviewActivityKey);
        deleted = true;
        const fourth = await connector.get(identity);
        assert.equal(typeof fourth.reviewActivityKey, "string");
        assert.notEqual(fourth.reviewActivityKey, third.reviewActivityKey);
        failPage = true;
        const incomplete = await connector.get(identity);
        assert.equal(incomplete.reviewThreads, "unknown");
        assert.equal(incomplete.reviewActivityKey, undefined);
    });
    test("nests review replies under top-level comments", async () => {
        const fetch = async (input) => {
            const url = String(input);
            if (url.includes("/issues/5/comments")) {
                return json([{
                        id: 20,
                        body: "summary",
                        user: { login: "review-bot" },
                        created_at: "2026-07-30T09:31:00Z",
                    }]);
            }
            if (url.includes("/pulls/5/comments")) {
                return json([
                    {
                        id: 10,
                        body: "finding",
                        user: { login: "review-bot" },
                        path: "src/app.ts",
                        original_line: 42,
                        created_at: "2026-07-30T09:30:00Z",
                    },
                    {
                        id: 11,
                        in_reply_to_id: 10,
                        body: "ccr:label=wrong",
                        user: { login: "owner" },
                        path: "src/app.ts",
                        created_at: "2026-07-30T09:32:00Z",
                    },
                ]);
            }
            return new Response("not found", { status: 404 });
        };
        const connector = new GitHubForgeReader({
            forge: "github.com",
            provider: "github",
            host: "github.com",
            token: "secret",
        }, { fetch });
        assert.deepEqual(wire(await connector.comments({ repository: { forge: { name: "github.com" }, path: "owner/repo" }, number: 5 })), wire([
            {
                id: "10",
                body: "finding",
                author: "review-bot",
                replyable: true,
                replies: [{
                        id: "11",
                        body: "ccr:label=wrong",
                        author: "owner",
                        replyable: false,
                        replies: [],
                        path: "src/app.ts",
                        createdAt: "2026-07-30T09:32:00Z",
                    }],
                path: "src/app.ts",
                line: 42,
                createdAt: "2026-07-30T09:30:00Z",
            },
            {
                id: "20",
                body: "summary",
                author: "review-bot",
                replyable: false,
                replies: [],
                createdAt: "2026-07-30T09:31:00Z",
            },
        ]));
    });
});
describe("GitLabForgeReader", () => {
    test("maps MR state and counts only resolvable discussions", async () => {
        const calls = [];
        const fetch = async (input, init) => {
            const url = String(input);
            calls.push({ url, headers: new Headers(init?.headers) });
            if (url.endsWith("/merge_requests/9")) {
                return json({
                    iid: 9,
                    title: "Keep Board focused",
                    web_url: "https://gitlab.example.com/group/subgroup/repo/-/merge_requests/9",
                    state: "opened",
                    detailed_merge_status: "mergeable",
                    has_conflicts: false,
                });
            }
            if (url.includes("/discussions?")) {
                return json([
                    {
                        id: "conversation",
                        notes: [{
                                id: 1,
                                resolvable: false,
                                resolved: false,
                            }],
                    },
                    {
                        id: "review",
                        notes: [{
                                id: 2,
                                updated_at: "2026-07-26T08:30:00Z",
                                resolvable: true,
                                resolved: false,
                            }],
                    },
                ]);
            }
            if (url.includes("/merge_requests?")) {
                const state = new URL(url).searchParams.get("state");
                return state === "opened"
                    ? json([{ iid: 9, state: "opened" }])
                    : json([{ iid: 8, state: "merged" }]);
            }
            return new Response("not found", { status: 404 });
        };
        const connector = new GitLabForgeReader({
            forge: "gitlab.example.com",
            provider: "gitlab",
            host: "gitlab.example.com",
            token: "secret",
        }, {
            fetch,
            now: () => new Date("2026-07-26T09:00:00.000Z"),
        });
        assertObservation(await connector.get({ repository: { forge: { name: "gitlab.example.com" }, path: "group/subgroup/repo" }, number: 9 }), {
            ...{ repository: { forge: { name: "gitlab.example.com" }, path: "group/subgroup/repo" }, number: 9 },
            title: "Keep Board focused",
            url: "https://gitlab.example.com/group/subgroup/repo/-/merge_requests/9",
            state: "open",
            reviewThreads: "unresolved",
            mergeability: "ready",
            observedAt: "2026-07-26T09:00:00.000Z"
        });
        assert.deepEqual(wire(await connector.list("group/subgroup/repo", {
            state: "open",
            limit: 3,
        })), wire([
            { repository: { forge: { name: "gitlab.example.com" }, path: "group/subgroup/repo" }, number: 9 },
        ]));
        assert.deepEqual(wire(await connector.list("group/subgroup/repo", {
            state: "merged",
            limit: 3,
        })), wire([
            { repository: { forge: { name: "gitlab.example.com" }, path: "group/subgroup/repo" }, number: 8 },
        ]));
        assert.equal(calls[0]?.headers.get("private-token"), "secret");
        assert.ok(calls.map((call) => call.url).includes("https://gitlab.example.com/api/v4/projects/" +
            "group%2Fsubgroup%2Frepo/merge_requests/9"));
        assert.equal(calls.some((call) => call.url.includes("/merge_requests?state=opened")), true);
        assert.equal(calls.some((call) => call.url.includes("/merge_requests?state=merged")), true);
    });
    test("does not treat ordinary conversation notes as review threads", async () => {
        const fetch = async (input) => {
            const url = String(input);
            if (url.endsWith("/merge_requests/4")) {
                return json({
                    iid: 4,
                    title: "Ordinary conversation",
                    web_url: "https://gitlab.example.com/group/repo/-/merge_requests/4",
                    state: "closed",
                    detailed_merge_status: "unchecked",
                    has_conflicts: false,
                });
            }
            if (url.includes("/discussions?")) {
                return json([{
                        id: "conversation",
                        notes: [{
                                id: 1,
                                resolvable: false,
                                resolved: false,
                            }],
                    }]);
            }
            return new Response("not found", { status: 404 });
        };
        const connector = new GitLabForgeReader({
            forge: "gitlab.example.com",
            provider: "gitlab",
            host: "gitlab.example.com",
            token: "secret",
        }, { fetch });
        assertFields(await connector.get({ repository: { forge: { name: "gitlab.example.com" }, path: "group/repo" }, number: 4 }), {
            state: "closed",
            reviewThreads: "none",
            mergeability: "unknown",
        });
    });
    test("applies the caller's MergeRequest state and result limit", async () => {
        const pages = [];
        const fetch = async (input) => {
            const url = new URL(String(input));
            const state = url.searchParams.get("state");
            const page = Number(url.searchParams.get("page"));
            pages.push(`${state}:${page}`);
            if (state === "opened" && page === 1) {
                return json([{ iid: 3, state: "opened" }], {
                    "x-next-page": "2",
                });
            }
            if (state === "opened") {
                return json([{ iid: 1, state: "opened" }]);
            }
            return json([{ iid: 2, state: "merged" }]);
        };
        const connector = new GitLabForgeReader({
            forge: "gitlab.example.com",
            provider: "gitlab",
            host: "gitlab.example.com",
            token: "secret",
        }, { fetch });
        assert.deepEqual(wire(await connector.list("group/repo", {
            state: "open",
            limit: 1,
        })), wire([
            { repository: { forge: { name: "gitlab.example.com" }, path: "group/repo" }, number: 3 },
        ]));
        assert.deepEqual(wire(await connector.list("group/repo", {
            state: "merged",
            limit: 1,
        })), wire([
            { repository: { forge: { name: "gitlab.example.com" }, path: "group/repo" }, number: 2 },
        ]));
        assert.deepEqual(wire(pages), wire(["opened:1", "merged:1"]));
    });
    test("optionally filters MergeRequests by configured author uids", async () => {
        let requestedUrl = "";
        const fetch = async (input) => {
            requestedUrl = String(input);
            return json([
                {
                    iid: 3,
                    state: "opened",
                    author: { username: "someone-else", id: 7 },
                },
                {
                    iid: 2,
                    state: "opened",
                    author: { username: "owner", id: 42 },
                },
                {
                    iid: 1,
                    state: "opened",
                    author: { username: "backup", id: 8 },
                },
            ]);
        };
        const connector = new GitLabForgeReader({
            forge: "gitlab.example.com",
            provider: "gitlab",
            host: "gitlab.example.com",
            token: "secret",
            uids: ["42", "backup"],
        }, { fetch });
        assert.deepEqual(wire(await connector.list("group/repo", {
            state: "open",
            limit: 2,
        })), wire([
            { repository: { forge: { name: "gitlab.example.com" }, path: "group/repo" }, number: 2 },
            { repository: { forge: { name: "gitlab.example.com" }, path: "group/repo" }, number: 1 },
        ]));
        const url = new URL(requestedUrl);
        assert.equal(url.searchParams.get("author_id"), null);
        assert.equal(url.searchParams.get("author_username"), null);
        assert.equal(url.searchParams.get("per_page"), "100");
    });
    test("nests discussion replies under top-level comments", async () => {
        const fetch = async () => json([
            {
                id: "discussion-1",
                individual_note: false,
                notes: [
                    {
                        id: 10,
                        body: "finding",
                        author: { username: "review-bot" },
                        created_at: "2026-07-30T09:30:00Z",
                        position: {
                            new_path: "src/app.ts",
                            new_line: 42,
                        },
                    },
                    {
                        id: 11,
                        body: "ccr:label=wrong",
                        author: { username: "owner" },
                        created_at: "2026-07-30T09:31:00Z",
                    },
                ],
            },
            {
                id: "plain",
                individual_note: true,
                notes: [{
                        id: 20,
                        body: "summary",
                        author: { username: "review-bot" },
                        created_at: "2026-07-30T09:32:00Z",
                    }],
            },
        ]);
        const connector = new GitLabForgeReader({
            forge: "gitlab.example.com",
            provider: "gitlab",
            host: "gitlab.example.com",
            token: "secret",
        }, { fetch });
        assert.deepEqual(wire(await connector.comments({ repository: { forge: { name: "gitlab.example.com" }, path: "group/repo" }, number: 5 })), wire([
            {
                id: "10",
                body: "finding",
                author: "review-bot",
                replyable: true,
                replies: [{
                        id: "11",
                        body: "ccr:label=wrong",
                        author: "owner",
                        replyable: false,
                        replies: [],
                        createdAt: "2026-07-30T09:31:00Z",
                    }],
                path: "src/app.ts",
                line: 42,
                createdAt: "2026-07-30T09:30:00Z",
            },
            {
                id: "20",
                body: "summary",
                author: "review-bot",
                replyable: false,
                replies: [],
                createdAt: "2026-07-30T09:32:00Z",
            },
        ]));
    });
});
export {};
