import { mkdtemp, mkdir, writeFile, rm, realpath, symlink } from "node:fs/promises";
import { readFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { test } from "node:test";
import assert from "node:assert/strict";
import { inspect, owner } from "../dist/index.js";

const gitPath = execFileSync("which", ["git"], { encoding: "utf8" }).trim();
function git(root, ...args) { return execFileSync(gitPath, ["-C", root, ...args], { encoding: "utf8", stdio: ["pipe", "pipe", "pipe"] }).trim(); }
async function write(root, files) {
  for (const [name, text] of Object.entries(files)) {
    await mkdir(dirname(join(root, name)), { recursive: true });
    await writeFile(join(root, name), text);
  }
}
async function repository(t, files = {}) {
  const root = await realpath(await mkdtemp(join(tmpdir(), "repocli-ts-")));
  t.after(() => rm(root, { recursive: true, force: true }));
  git(root, "init", "-q");
  await write(root, files);
  return root;
}
function commit(root) {
  git(root, "add", ".");
  git(root, "-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-qm", "fixture");
}

const layouts = JSON.parse(readFileSync(new URL("../../../conformance/inspect/layouts.json", import.meta.url)));
for (const item of layouts) {
  test(`native Git inspection: ${item.name}`, async t => {
    const root = await repository(t, item.files);
    if (item.origin) git(root, "remote", "add", "origin", item.origin);
    commit(root);
    for (const options of [{}, { staged: true }, { head: "HEAD" }]) {
      if (item.error) { await assert.rejects(inspect({ repository: root, ...options })); continue; }
      const report = await inspect({ repository: root, ...options });
      assert.deepEqual({ repository: report.repository, components: report.components }, item.expected);
      assert.equal(report.complete, true);
      assert.equal(report.checkout, root);
      for (const [path, name] of Object.entries(item.owners)) assert.equal(owner(report, path)?.name ?? null, name);
    }
  });
}

test("shared committed/index/working metadata transitions", async t => {
  const fixture = JSON.parse(readFileSync(new URL("../../../conformance/inspect/versions.json", import.meta.url)));
  const root = await repository(t, fixture.commit);
  commit(root);
  const head = git(root, "rev-parse", "HEAD");
  await write(root, fixture.index);
  git(root, "add", ".");
  await write(root, fixture.working_tree);
  for (const options of [{}, { staged: true }, { head: "HEAD" }]) {
    const report = await inspect({ repository: root, ...options });
    assert.equal(report.components[0].language, fixture.expected[report.input]);
    assert.equal(report.head, options.head ? head : undefined);
    assert.equal(report.complete, true);
  }
});

test("large source contents, ignored manifests and gitlinks stay outside the read set", async t => {
  const root = await repository(t, { "main.go": "x".repeat(3 << 20), "go.mod": "", ".gitignore": "ignored/\n" });
  commit(root);
  await write(root, { "ignored/package.json": "{" });
  const oid = git(root, "rev-parse", "HEAD");
  git(root, "update-index", "--add", "--cacheinfo", `160000,${oid},child`);
  await mkdir(join(root, "child"));
  git(join(root, "child"), "init", "-q");
  await write(root, { "child/package.json": "x".repeat(3 << 20) });
  for (const options of [{}, { staged: true }]) {
    const report = await inspect({ repository: root, ...options });
    assert.equal(report.components.length, 1);
    assert.equal(report.components[0].language, "go");
  }
});

test("metadata limits reject before decoding, for all selected versions", async t => {
  const root = await repository(t, { "package.json": "x".repeat((2 << 20) + 1) });
  commit(root);
  for (const options of [{}, { staged: true }, { head: "HEAD" }]) await assert.rejects(inspect({ repository: root, ...options }), /exceeds/);
});

test("metadata symlinks are rejected and ordinary symlinks are skipped", async t => {
  const root = await repository(t, { "outside": "{}" });
  await symlink("outside", join(root, "source.py"));
  let report = await inspect({ repository: root });
  assert.deepEqual(report.components, []);
  await symlink("outside", join(root, "package.json"));
  commit(root);
  for (const options of [{}, { staged: true }, { head: "HEAD" }]) await assert.rejects(inspect({ repository: root, ...options }), /symlink|regular file/);
});

test("symlinked ancestor cannot supply working metadata", async t => {
  const root = await repository(t, { "nested/package.json": "{}", "outside/package.json": "{}" });
  commit(root);
  await rm(join(root, "nested"), { recursive: true });
  await symlink("outside", join(root, "nested"));
  await assert.rejects(inspect({ repository: root }), /symlinked directory/);
  assert.equal((await inspect({ repository: root, staged: true })).complete, true);
});

test("unmerged index remains an error", async t => {
  const root = await repository(t, { "main.go": "package main" });
  commit(root);
  const oid = git(root, "rev-parse", "HEAD:main.go");
  execFileSync(gitPath, ["-C", root, "update-index", "--index-info"], { input: `0 ${"0".repeat(40)}\tmain.go\n100644 ${oid} 1\tmain.go\n100644 ${oid} 2\tmain.go\n` });
  await assert.rejects(inspect({ repository: root }), /unmerged/);
  await assert.rejects(inspect({ repository: root, staged: true }), /unmerged/);
});

test("bad selections and invalid repositories reject without fallback", async t => {
  const root = await repository(t);
  await assert.rejects(inspect({ repository: root, staged: true, head: "HEAD" }), /mutually exclusive/);
  await assert.rejects(inspect({ repository: root, head: "missing" }));
  await assert.rejects(inspect({ repository: join(root, "missing") }));
  await assert.rejects(inspect({ repository: root, timeoutMs: 0 }), /timeoutMs/);
  const controller = new AbortController();
  controller.abort(new Error("caller cancelled"));
  await assert.rejects(inspect({ repository: root, signal: controller.signal }), /caller cancelled/);
});

async function interceptGit(t, root, body) {
  const bin = join(root, "fake-bin");
  await mkdir(bin);
  await writeFile(join(bin, "git"), `#!${process.execPath}\n${body}\n`, { mode: 0o755 });
  const previous = process.env.PATH;
  process.env.PATH = bin + ":" + previous;
  t.after(() => { process.env.PATH = previous; });
}

test("concurrent metadata edits return incomplete with the original observation", async t => {
  const root = await repository(t, { "package.json": "{}" });
  commit(root);
  await interceptGit(t, root, `
    const {execFileSync} = require('node:child_process');
    const {writeFileSync} = require('node:fs');
    const args = process.argv.slice(2);
    if (args.includes('config')) {
      writeFileSync(${JSON.stringify(join(root, "package.json"))}, '{"dependencies":{"typescript":"*"}}');
      process.exit(1);
    }
    process.stdout.write(execFileSync(${JSON.stringify(gitPath)}, args));
  `);
  const report = await inspect({ repository: root });
  assert.equal(report.complete, false);
  assert.equal(report.components[0].language, "javascript");
  assert.equal(report.diagnostics[0].code, "inspection_changed");
});

test("in-flight Git respects a whole-operation deadline and caller abort", async t => {
  const root = await repository(t);
  await interceptGit(t, root, "setInterval(() => {}, 1000);");
  const started = Date.now();
  await assert.rejects(inspect({ repository: root, timeoutMs: 100 }), /abort/i);
  assert.ok(Date.now() - started < 2000);
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 100);
  try { await assert.rejects(inspect({ repository: root, signal: controller.signal }), /abort/i); }
  finally { clearTimeout(timer); }
});

test("file and aggregate metadata budgets also bound immutable index input", async t => {
  const root = await repository(t, { "package.json": " ".repeat(2 << 20) });
  commit(root);
  const oid = git(root, "rev-parse", "HEAD:package.json");
  let entries = "";
  for (let i = 0; i < 65; i++) entries += `100644 ${oid}\tcomponent-${i}/package.json\n`;
  execFileSync(gitPath, ["-C", root, "update-index", "--index-info"], { input: entries });
  await assert.rejects(inspect({ repository: root, staged: true }), /128 MiB/);
  entries = "";
  for (let i = 0; i < 10_001; i++) entries += `100644 ${oid}\tsource-${i}.go\n`;
  execFileSync(gitPath, ["-C", root, "update-index", "--index-info"], { input: entries });
  await assert.rejects(inspect({ repository: root, staged: true }), /10000 files/);
});
