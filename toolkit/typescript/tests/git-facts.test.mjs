import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync, rmSync, realpathSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { execFileSync } from "node:child_process";
import { committedPaths, rangePaths, parseRemoteUrl, remote, checkoutInfo, listWorktrees } from "../dist/index.js";

function fixture(t) {
  const root = realpathSync(mkdtempSync(join(tmpdir(), "repocli-facts-")));
  const git = (...args) => execFileSync("git", ["-C", root, ...args], {encoding: "utf8"}).trim();
  git("init", "-q"); git("config", "user.name", "Fixture"); git("config", "user.email", "fixture@example.invalid");
  git("config", "core.hooksPath", "/dev/null");
  writeFileSync(join(root, "中文.py"), "pass\n"); git("add", "."); git("commit", "-qm", "initial");
  t.after(() => rmSync(root, {recursive:true, force:true}));
  return { root, git };
}

test("history paths preserve unicode, whitespace and both rename sides", t => {
  const {root, git} = fixture(t);
  assert.deepEqual(committedPaths(root), ["中文.py"]);
  const base = git("rev-parse", "HEAD");
  const name = " spaced\n中文.py ";
  git("mv", "中文.py", name); git("commit", "-qm", "rename");
  assert.deepEqual(new Set(committedPaths(root)), new Set(["中文.py", name]));
  assert.deepEqual(new Set(rangePaths(root, base)), new Set(["中文.py", name]));
  assert.throws(() => rangePaths(root, "missing"));
});

test("range uses merge base and commit uses first parent", t => {
  const {root, git} = fixture(t);
  const base = git("rev-parse", "HEAD");
  git("checkout", "-qb", "target");
  writeFileSync(join(root, "target"), "target"); git("add", "."); git("commit", "-qm", "target");
  git("checkout", "-qb", "feature", base);
  writeFileSync(join(root, "feature"), "feature"); git("add", "."); git("commit", "-qm", "feature");
  assert.deepEqual(rangePaths(root, "target"), ["feature"]);
  git("merge", "--no-ff", "-m", "merge", "target");
  assert.deepEqual(committedPaths(root), ["target"]);
});

test("remote formats share identity", t => {
  const {root, git} = fixture(t);
  const urls = ["git@github.com:org/sub/repo.git", "ssh://git@github.com/org/sub/repo.git", "https://user:secret@github.com/org/sub/repo.git"];
  urls.push("git+ssh://git@github.com/org/sub/repo.git", "git+https://github.com/org/sub/repo.git",
    "ssh+git://git@github.com/org/sub/repo.git", "custom://github.com/org/sub/repo.git");
  for (const url of urls) assert.deepEqual(parseRemoteUrl(url), {host:"github.com", path:"org/sub/repo"});
  git("remote", "add", "origin", urls[1]);
  assert.deepEqual(remote(root), parseRemoteUrl(urls[1]));
  for (const url of ["file:///tmp/repo", "/tmp/a:b", "../repo", "https://"]) assert.equal(parseRemoteUrl(url), undefined);
});

test("topology distinguishes checkout from separate metadata", t => {
  const {root, git} = fixture(t);
  const linked = root + " 中文\n ";
  const metadata = root + "-metadata";
  t.after(() => { rmSync(linked, {recursive:true, force:true}); rmSync(metadata, {recursive:true, force:true}); });
  git("worktree", "add", "-qb", "linked", linked);
  assert.equal(checkoutInfo(linked).mainRoot, root);
  assert.equal(listWorktrees(root)[1].path, linked);
  git("worktree", "remove", linked);
  git("init", "--separate-git-dir", metadata);
  git("worktree", "add", linked, "linked");
  for (const path of [root, linked]) {
    const info = checkoutInfo(path);
    assert.equal(info.commonDir, metadata);
    assert.equal(info.mainRoot, undefined);
  }
  assert.throws(() => listWorktrees(root + "/missing"));
});
