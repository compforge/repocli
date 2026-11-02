import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { runGit, localDefaultBranch, addExclude, gitPath, checkoutAt, refExists, isIgnored, hasUnstagedChanges } from "../dist/index.js";

test("repository queries retain absence, failures and literal ignore rules", () => {
  const repo = mkdtempSync(join(tmpdir(), "repokit-queries-"));
  const git = (...args) => { const r = runGit(repo, args); assert.equal(r.ok, true, r.stderr); return r.stdout; };
  try {
    git("init", "-qb", "main");
    git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "init");
    assert.equal(localDefaultBranch(repo), undefined);
    git("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/release/stable");
    assert.equal(localDefaultBranch(repo), "release/stable");
    assert.equal(refExists(repo, "refs/heads/main"), true);
    assert.equal(refExists(repo, "refs/tags/missing"), false);
    assert.equal(hasUnstagedChanges(repo), false);
    writeFileSync(join(repo, " file\n.txt "), "data");
    assert.equal(hasUnstagedChanges(repo), true);
    git("add", ".");
    assert.equal(hasUnstagedChanges(repo), false);
    writeFileSync(join(repo, " file\n.txt "), "changed");
    assert.equal(hasUnstagedChanges(repo), true);
    const exclude = gitPath(repo, "info/exclude");
    writeFileSync(exclude, Buffer.from([0xff, 10]));
    assert.equal(addExclude(repo, "/ignored/"), true);
    assert.equal(addExclude(repo, "/ignored/"), false);
    assert.equal(readFileSync(exclude)[0], 0xff);
    assert.equal(isIgnored(repo, "ignored/file"), true);
    assert.equal(isIgnored(repo, "tracked"), false);
    assert.throws(() => addExclude(repo, "one\ntwo"));
    assert.ok(checkoutAt(repo));
    const child = join(repo, "child"); mkdirSync(child);
    assert.equal(checkoutAt(child), undefined);
    writeFileSync(join(child, ".git"), "gitdir: nonexistent");
    assert.throws(() => checkoutAt(child));
    assert.throws(() => localDefaultBranch(child));
    assert.throws(() => refExists(child, "refs/heads/main"));
    assert.throws(() => isIgnored(child, "f"));
    assert.throws(() => hasUnstagedChanges(child));
  } finally { rmSync(repo, { recursive: true, force: true }); }
});
