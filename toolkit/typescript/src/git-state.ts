import { existsSync, realpathSync, lstatSync, statSync } from "node:fs";
import { join, dirname, basename } from "node:path";
import { runGit } from "./operations.js";

/** Absolute metadata path, including absent paths; Git owns checkout/shared placement. */
export function gitPath(repo: string, name: string): string {
  const result = runGit(repo, ["rev-parse", "--path-format=absolute", "--git-path", name], 5000, true);
  if (!result.ok || !result.stdout) throw new Error(`cannot resolve Git metadata path ${JSON.stringify(name)}: ${result.stderr}`);
  // Remove only Git's record terminator, never whitespace belonging to the path.
  return result.stdout.replace(/\n$/, "");
}

export interface WorkspaceStatus {
  readonly dirty: boolean;
  readonly complete: boolean;
  readonly modifiedCount: number;
  readonly untrackedCount: number;
}

export interface WorktreeEntry {
  readonly path: string;
  readonly sha: string;
  readonly branch?: string;
}

/** Current branch, including unborn branches; undefined means detached HEAD. */
export function currentBranch(repo: string): string | undefined {
  const result = runGit(repo, ["branch", "--show-current"]);
  if (!result.ok) throw new Error(`cannot read current branch: ${result.stderr}`);
  return result.stdout || undefined;
}

function divergence(repo: string, target: string): readonly [number, number] | undefined {
  const head = revParse(repo, "HEAD"); const other = revParse(repo, target);
  if (!head || !other) return undefined;
  const result = runGit(repo, ["rev-list", "--count", "--left-right", `${head}...${other}`]);
  const values = result.stdout.split(/\s+/);
  if (!result.ok || values.length !== 2 || !values.every(v => /^\d+$/.test(v))) {
    throw new Error(`cannot count divergence from ${target}: ${result.stderr || result.stdout}`);
  }
  return [Number(values[0]), Number(values[1])];
}

/** Undefined for absent endpoints; execution/parse failures throw. */
export function aheadBehind(repo: string, target = "main"): readonly [number, number] | undefined {
  return divergence(repo, `refs/remotes/origin/${target}`);
}

export function workspaceStatus(repo: string): WorkspaceStatus {
  const result = runGit(repo, ["status", "--porcelain"]);
  if (!result.ok) return { dirty: false, complete: false, modifiedCount: 0, untrackedCount: 0 };
  const lines = result.stdout.split("\n").filter(Boolean);
  return {
    dirty: lines.length > 0,
    complete: true,
    modifiedCount: lines.filter((line) => !line.startsWith("??")).length,
    untrackedCount: lines.filter((line) => line.startsWith("??")).length,
  };
}

/** Resolve a revision; empty means absent, not a failed observation. */
export function revParse(repo: string, ref: string): string {
  const result = runGit(repo, ["rev-parse", "--verify", "--quiet", "--end-of-options", ref]);
  if (result.ok && result.stdout) return result.stdout;
  if (result.code === 1 && !result.stderr) return "";
  throw new Error(`cannot resolve revision ${ref}: ${result.stderr || result.stdout}`);
}

/** Empty for unborn HEAD; failed reads throw. */
export function headSha(repo: string): string { return revParse(repo, "HEAD"); }

export function targetExists(repo: string, target = "main"): boolean {
  return revParse(repo, `refs/remotes/origin/${target}`) !== "";
}

export function refreshRemoteHead(repo: string, timeoutMs = 5_000): boolean {
  return runGit(repo, ["remote", "set-head", "origin", "--auto"], timeoutMs).ok;
}

export function setLocalDefaultHead(repo: string, branch: string): boolean {
  return branch !== "" && runGit(repo, ["symbolic-ref", "refs/remotes/origin/HEAD", `refs/remotes/origin/${branch}`]).ok;
}

/** Invalid objects and failed reads throw, including identical invalid revisions. */
export function isAncestor(repo: string, ancestor?: string, descendant?: string): boolean {
  if (!ancestor || !descendant) throw new Error("ancestry requires two revisions");
  const result = runGit(repo, ["merge-base", "--is-ancestor", "--", ancestor, descendant]);
  if (result.code === 0 || result.code === 1) return result.code === 0;
  throw new Error(`cannot compare ancestry ${ancestor} -> ${descendant}: ${result.stderr}`);
}

/** Undefined when branch/upstream is absent; failed reads throw. */
export function upstreamAheadBehind(repo: string): readonly [number, number] | undefined {
  const branch = currentBranch(repo);
  if (branch === undefined) return undefined;
  const ref = `refs/heads/${branch}`;
  const result = runGit(repo, ["for-each-ref", "--format=%(refname)%00%(upstream)", ref]);
  if (!result.ok) throw new Error(`cannot read upstream for ${branch}: ${result.stderr}`);
  const upstream = result.stdout.split("\n").find(line => line.startsWith(ref + "\0"))?.split("\0")[1];
  return upstream ? divergence(repo, upstream) : undefined;
}

export function remoteTips(repo: string, branches: readonly string[], timeoutMs = 5_000): ReadonlyMap<string, string> {
  if (branches.length === 0) return new Map();
  const result = runGit(repo, ["ls-remote", "origin", ...branches], timeoutMs);
  const tips = new Map<string, string>();
  if (!result.ok) throw new Error(result.stderr || "cannot read remote branch tips");
  for (const line of result.stdout.split("\n")) {
    const [sha, ref] = line.split("\t");
    if (sha && ref?.startsWith("refs/heads/")) tips.set(ref.slice("refs/heads/".length), sha);
  }
  return tips;
}

export function listWorktrees(repo: string): readonly WorktreeEntry[] {
  const result = runGit(repo, ["worktree", "list", "--porcelain", "-z"]);
  if (!result.ok) throw new Error(result.stderr || "cannot list worktrees");
  const entries: WorktreeEntry[] = [];
  for (const block of result.stdout.split("\0\0")) {
    const fields = new Map(block.split("\0").filter(line => line.includes(" ")).map(line => {
      const space = line.indexOf(" ");
      return [line.slice(0, space), line.slice(space + 1)] as const;
    }));
    const path = fields.get("worktree");
    const branch = fields.get("branch")?.replace(/^refs\/heads\//, "");
    if (path) entries.push({ path, sha: fields.get("HEAD") ?? "", ...(branch ? { branch } : {}) });
  }
  return entries;
}

export interface CheckoutInfo {
  readonly root: string;
  readonly gitDir: string;
  readonly commonDir: string;
  readonly mainRoot?: string;
  readonly linked: boolean;
}

/** Shared metadata may not identify the original checkout of a separate Git directory. */
export function checkoutInfo(repo: string): CheckoutInfo {
  const query = (path: string, flag: string): string => {
    const result = runGit(path, ["rev-parse", "--path-format=absolute", flag], 5_000, true);
    if (!result.ok || !result.stdout) throw new Error(result.stderr || `cannot resolve ${flag}`);
    return realpathSync(result.stdout.replace(/\n$/, ""));
  };
  const root = query(repo, "--show-toplevel");
  if (!existsSync(join(root, ".git"))) throw new Error("Git metadata directory is not a checkout");
  const gitDir = query(repo, "--git-dir");
  const commonDir = query(repo, "--git-common-dir");
  const first = listWorktrees(repo)[0];
  let mainRoot: string | undefined;
  if (first) {
    try {
      const candidate = query(first.path, "--show-toplevel");
      if (existsSync(join(candidate, ".git")) && query(candidate, "--git-common-dir") === commonDir) mainRoot = candidate;
    } catch { /* Missing or prunable checkout: leave mainRoot unknown. */ }
  }
  return { root, gitDir, commonDir, linked: gitDir !== commonDir, ...(mainRoot ? { mainRoot } : {}) };
}

export interface CheckoutEntry {
  /** Unknown when shared metadata cannot locate the primary checkout. */
  readonly path?: string;
  readonly sha: string;
  readonly branch?: string;
  readonly primary: boolean;
}

/** Checkout locations, preserving unknown primary paths; excludes bare registrations. */
export function listCheckouts(repo: string): readonly CheckoutEntry[] {
  const info = checkoutInfo(repo);
  const main = info.linked ? info.mainRoot : info.root;
  return listWorktrees(repo).flatMap((entry, index) => {
    if (!entry.sha) return []; // Bare repository metadata has no checkout HEAD.
    const path = index === 0 ? main : entry.path;
    return [{ ...(path === undefined ? {} : { path }), sha: entry.sha,
      ...(entry.branch ? { branch: entry.branch } : {}), primary: index === 0 }];
  });
}

export function mainRepoRoot(repo: string): string | undefined {
  return checkoutInfo(repo).mainRoot;
}

export function worktreeMetadata(repo: string): { readonly linked: boolean; readonly commonDir: string; readonly mainBranch?: string } {
  const info = checkoutInfo(repo);
  if (!info.linked) return { linked: false, commonDir: "" };
  const mainBranch = listCheckouts(repo).find(entry => entry.primary)?.branch;
  return { linked: true, commonDir: info.commonDir, ...(mainBranch ? { mainBranch } : {}) };
}

export function localBranches(repo: string): ReadonlyMap<string, string> {
  const result = runGit(repo, ["for-each-ref", "--sort=refname", "--format=%(refname:short)%00%(objectname)", "refs/heads"]);
  const branches = new Map<string, string>();
  if (!result.ok) throw new Error(result.stderr || "cannot list local branches");
  for (const line of result.stdout.split("\n")) {
    const [name, sha] = line.split("\0");
    if (name && sha) branches.set(name, sha);
  }
  return branches;
}

export function fetchRemote(repo: string, refs: readonly string[] = [], timeoutMs = 8_000): boolean {
  return runGit(repo, ["fetch", "origin", ...refs, "--quiet"], timeoutMs).ok;
}

/** Keep runtime state local without editing the repository's committed ignore file. */

/** Find a physical ancestor checkout. Absence is undefined; invalid paths,
 * inaccessible/corrupt metadata and Git failures throw instead of implying absence.
 */
export function findCheckout(directory: string): string | undefined {
  const start = realpathSync(directory);
  if (!statSync(start).isDirectory()) throw new Error(`not a directory: ${directory}`);
  for (let candidate = start; ; candidate = dirname(candidate)) {
    if (basename(candidate) === ".git") return undefined;
    let present = true;
    try { lstatSync(join(candidate, ".git")); }
    catch (error) { if ((error as NodeJS.ErrnoException).code === "ENOENT") present = false; else throw error; }
    if (present) {
      const result = runGit(candidate, ["rev-parse", "--show-toplevel"], 5_000, true);
      if (!result.ok || !result.stdout) throw new Error(result.stderr || "cannot discover checkout");
      const root = result.stdout.replace(/\n$/, "");
      if (realpathSync(root) !== candidate) throw new Error("Git environment does not match discovered checkout");
      return root;
    }
    if (dirname(candidate) === candidate) return undefined;
  }
}
