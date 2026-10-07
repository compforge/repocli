import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, readFileSync, realpathSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { execFileSync } from "node:child_process";
import test from "node:test";
import { checkoutInfo, listCheckouts, localBranches, remoteTips } from "../dist/index.js";

const cases = JSON.parse(readFileSync(new URL("../../../conformance/git/checkouts.json", import.meta.url)));
for (const fixture of cases) test(`checkout contract: ${fixture.name}`, t => {
  const directory = realpathSync(mkdtempSync(join(tmpdir(), "checkout-contract-")));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const paths = { root: join(directory, "repo"), linked: join(directory, " linked\n工作区 "),
    metadata: join(directory, "metadata"), missing: join(directory, "missing") };
  mkdirSync(paths.root);
  const git = (cwd, ...args) => execFileSync("git", ["-C", cwd, ...args], { stdio: "pipe" });
  git(paths.root, "init", "-qb", "main");
  git(paths.root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid",
    "-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-qm", "initial");
  if (fixture.mode === "separate") git(paths.root, "init", "--separate-git-dir", paths.metadata);
  if (fixture.mode === "bare") git(paths.root, "clone", "--bare", paths.root, paths.metadata);
  git(fixture.mode === "bare" ? paths.metadata : paths.root, "worktree", "add", "-qb", "topic", paths.linked, "HEAD");
  const view = paths[fixture.view];
  if (fixture.error) {
    assert.throws(() => checkoutInfo(view)); assert.throws(() => listCheckouts(view)); return;
  }
  const label = value => value === undefined ? null : Object.keys(paths).find(key => paths[key] === value) ?? value;
  const info = checkoutInfo(view);
  assert.deepEqual({ root: label(info.root), mainRoot: label(info.mainRoot), linked: info.linked,
    entries: listCheckouts(view).map(e => ({ path: label(e.path), branch: e.branch, primary: e.primary })) }, fixture.expected);
});

test("failed inventory queries are not empty collections", t => {
  const directory = mkdtempSync(join(tmpdir(), "checkout-failure-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  assert.throws(() => localBranches(directory));
  assert.throws(() => remoteTips(directory, ["main"]));
  execFileSync("git", ["-C", directory, "init", "-q"]);
  assert.equal(localBranches(directory).size, 0); // An unborn repository really has no branches.
});
