import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, writeFileSync, symlinkSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { execFileSync } from "node:child_process";
import { snapshot, changedPaths } from "../dist/index.js";

test("content identity follows unicode bytes across clean commits", async () => {
  const root = mkdtempSync(join(tmpdir(), "repocli-content-"));
  const git = (...args) => execFileSync("git", ["-C", root, ...args]);
  try {
    git("init", "-q"); git("config", "user.name", "Fixture"); git("config", "user.email", "fixture@example.invalid");
    git("config", "core.hooksPath", "/dev/null");
    writeFileSync(join(root, "测试.py"), "print(1)\n");
    git("add", "."); git("commit", "-qm", "initial");
    const first = await snapshot(root);
    assert.equal(first.complete, true);
    writeFileSync(join(root, "测试.py"), "print(2)\n");
    assert.deepEqual(changedPaths(root), ["测试.py"]);
    const second = await snapshot(root);
    assert.notEqual(first.digest, second.digest);
    git("add", "."); git("commit", "-qm", "second");
    assert.equal((await snapshot(root)).digest, second.digest);
    symlinkSync("测试.py", join(root, "alias"));
    assert.equal((await snapshot(root)).complete, true);
    symlinkSync("/etc/hosts", join(root, "outside"));
    assert.equal((await snapshot(root)).complete, false);
  } finally { rmSync(root, {recursive:true, force:true}); }
});


test("shared Go working-tree digest", async () => {
  const fixture = JSON.parse(readFileSync(new URL("../../../conformance/snapshot/working.json", import.meta.url), "utf8"));
  const root = mkdtempSync(join(tmpdir(), "repocli-conformance-"));
  try {
    execFileSync("git", ["-C", root, "init", "-q"]);
    for (const [name, content] of Object.entries(fixture.files)) writeFileSync(join(root, name), content);
    for (const [name, target] of Object.entries(fixture.symlinks)) symlinkSync(target, join(root, name));
    writeFileSync(join(root, fixture.large.path), Buffer.alloc(fixture.large.size, fixture.large.byte));
    const observed = await snapshot(root);
    assert.equal(observed.complete, true);
    assert.equal(observed.digest, fixture.digest);
  } finally { rmSync(root, { recursive: true, force: true }); }
});
