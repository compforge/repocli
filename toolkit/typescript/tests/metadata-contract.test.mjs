import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, readFileSync, realpathSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { execFileSync } from "node:child_process";
import test from "node:test";
import { gitPath } from "../dist/index.js";

const contract = JSON.parse(readFileSync(new URL("../../../conformance/git/metadata.json", import.meta.url)));
for (const fixture of contract.layouts) test(`metadata contract: ${fixture.name}`, t => {
  const directory = realpathSync(mkdtempSync(join(tmpdir(), "metadata-contract-")));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const root = join(directory, " 仓库\n ");
  const linked = join(directory, "linked");
  const metadata = join(directory, " metadata ");
  mkdirSync(root);
  const git = (cwd, ...args) => execFileSync("git", ["-C", cwd, ...args], { stdio: "pipe" });
  git(root, "init", "-qb", "main");
  git(root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid",
    "-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-qm", "initial");
  let common = join(root, ".git");
  if (fixture.mode === "separate") { git(root, "init", "--separate-git-dir", metadata); common = metadata; }
  if (fixture.mode === "bare") { git(root, "clone", "--bare", root, metadata); common = metadata; }
  git(fixture.mode === "bare" ? metadata : root, "worktree", "add", "-qb", "topic", linked, "HEAD");
  const view = fixture.view === "linked" ? linked : root;
  const local = fixture.view === "linked" ? join(common, "worktrees/linked") : common;
  for (const entry of contract.paths) {
    assert.equal(gitPath(view, entry.name), join(entry.shared ? common : local, entry.name));
  }
});

test("metadata lookup failures throw", t => {
  const directory = mkdtempSync(join(tmpdir(), "metadata-failure-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  assert.throws(() => gitPath(directory, "index"));
});
