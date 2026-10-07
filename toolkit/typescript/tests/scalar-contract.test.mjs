import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { execFileSync } from "node:child_process";
import test from "node:test";
import * as state from "../dist/index.js";

const cases = JSON.parse(readFileSync(new URL("../../../conformance/git/scalars.json", import.meta.url)));
const queries = { branch: state.currentBranch, head: state.headSha, ref: state.revParse,
  target: state.targetExists, ahead: state.aheadBehind, upstream: state.upstreamAheadBehind, ancestor: state.isAncestor };
for (const fixture of cases) test(`scalar contract: ${fixture.name}`, t => {
  const root = mkdtempSync(join(tmpdir(), "scalar-query-"));
  t.after(() => rmSync(root, {recursive:true, force:true}));
  const git = (...args) => execFileSync("git", ["-C",root,...args], {stdio:"pipe"});
  if (fixture.mode !== "nonrepo") {
    git("init", "-qb", "main");
    git("config", "user.name", "Fixture"); git("config", "user.email", "fixture@example.invalid");
    git("config", "commit.gpgsign", "false"); git("config", "core.hooksPath", "/dev/null");
    if (fixture.mode !== "unborn") git("commit", "--allow-empty", "-qm", "base");
    if (fixture.mode === "detached") git("checkout", "--detach", "-q");
    if (fixture.mode === "diverged") {
      git("checkout", "-qb", "other"); git("commit", "--allow-empty", "-qm", "other");
      git("update-ref", "refs/remotes/origin/main", "HEAD"); git("checkout", "-q", "main");
      git("commit", "--allow-empty", "-qm", "main");
      git("remote", "add", "origin", "https://example.invalid/repo");
      git("branch", "--set-upstream-to", "origin/main");
    }
  }
  const query = () => queries[fixture.op](root,...fixture.args);
  if (fixture.error) { assert.throws(query); return; }
  let value = query() ?? null;
  if (fixture.op === "head" && value) value = "present";
  assert.deepEqual(value, fixture.expected);
});

test("process start failures do not become query values", t => {
  const original = process.env.PATH;
  t.after(() => { process.env.PATH = original; });
  process.env.PATH = "/nonexistent-scalar-contract-path";
  for (const [name, query] of Object.entries(queries)) {
    assert.throws(() => query(tmpdir(), ...(name === "ancestor" ? ["HEAD","HEAD"] : name === "ref" ? ["HEAD"] : [])), name);
  }
});
