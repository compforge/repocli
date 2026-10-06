import { spawnSync } from "node:child_process";

export interface GitResult {
  readonly code: number;
  readonly stdout: string;
  readonly stderr: string;
  readonly ok: boolean;
  readonly uncertain: boolean;
}

/** A timed-out write may have taken effect; inspect before retrying it. */
export function runGit(repo: string, args: readonly string[], timeoutMs = 5_000): GitResult {
  const result = spawnSync("git", ["-C", repo, ...args], {
    encoding: "utf8", timeout: timeoutMs, maxBuffer: 16 << 20,
  });
  const code = result.status ?? -1;
  return { code, stdout: result.stdout?.includes("\0") ? result.stdout : (result.stdout ?? "").trim(),
    stderr: (result.stderr || result.error?.message || "").trim(), ok: code === 0,
    uncertain: (result.error as NodeJS.ErrnoException | undefined)?.code === "ETIMEDOUT" };
}

export function changedPaths(repo: string): readonly string[] {
  const tracked = runGit(repo, ["diff", "--no-renames", "--name-only", "-z", "HEAD", "--"]);
  const untracked = runGit(repo, ["ls-files", "--others", "--exclude-standard", "-z"]);
  if (!tracked.ok || !untracked.ok) throw new Error(tracked.stderr || untracked.stderr);
  return [...new Set((tracked.stdout + untracked.stdout).split("\0").filter(p => p && !p.endsWith("/")))];
}
