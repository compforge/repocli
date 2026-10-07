import { lstat, realpath } from "node:fs/promises";
import { dirname, join, posix } from "node:path";
import { isDeepStrictEqual } from "node:util";
import { Git } from "./git.js";
import { compare, manifestEcosystem, validPath } from "./layout.js";
import type { Diagnostic, InspectOptions } from "./model.js";

/** A directory implied by repository entries, including the root '.'. */
export interface Directory { readonly path: string; }
/** Project metadata, regardless of Component discovery exclusions. */
export interface Manifest { readonly path: string; readonly ecosystem: string; }
export interface File {
  readonly path: string;
  readonly kind: "regular" | "symlink" | "gitlink";
  readonly mode: string;
  readonly oid?: string;
  readonly role?: string;
  readonly manifest?: Manifest;
}
export interface TreeReport {
  readonly schemaVersion: 1;
  readonly checkout: string;
  readonly input: "working_tree" | "index" | "commit";
  readonly head?: string;
  readonly directories: readonly Directory[];
  readonly files: readonly File[];
  readonly complete: boolean;
  readonly diagnostics: readonly Diagnostic[];
}

/** Versioned entries, independent of Component configuration or source analysis.
 * Working regular files have no OID. Gitlinks use checkout HEAD when initialized,
 * otherwise retaining the index reference.
 * Empty directories are outside Git's catalog. Never enters child repositories.
 */
export async function tree(options: InspectOptions = {}): Promise<TreeReport> {
  if (options.head && options.staged) throw new Error("head and staged are mutually exclusive");
  const timeout = AbortSignal.timeout(options.timeoutMs ?? 5_000);
  const signal = options.signal ? AbortSignal.any([timeout, options.signal]) : timeout;
  const root = await realpath((await new Git(options.repository ?? process.cwd(), signal).run(["rev-parse", "--show-toplevel"])).toString().replace(/\n$/, ""));
  const git = new Git(root, signal);
  const head = options.head ? (await git.run(["rev-parse", "--verify", "--end-of-options", options.head + "^{commit}"])).toString().trim() : "";
  const files = await entries(git, head, options.staged ?? false);
  const complete = head !== "" || isDeepStrictEqual(files, await entries(git, head, options.staged ?? false));
  const directories = new Set(["."]);
  for (const file of files) for (let dir = posix.dirname(file.path); dir !== "."; dir = posix.dirname(dir)) directories.add(dir);
  return { schemaVersion: 1, checkout: root, input: head ? "commit" : options.staged ? "index" : "working_tree",
    ...(head ? { head } : {}), files, directories: [...directories].sort(compare).map(path => ({ path })), complete,
    diagnostics: complete ? [] : [{ code: "tree_changed", message: "repository entries changed during observation" }] };
}

async function entries(git: Git, head: string, staged: boolean): Promise<File[]> {
  const committed = head !== "" || staged;
  const output = await git.run(head ? ["ls-tree", "-r", "-z", "--full-tree", head] : ["ls-files", "--stage", "-z"]);
  const catalog = new Map<string, readonly [string, string]>();
  for (const raw of output.toString().split("\0")) {
    if (!raw) continue;
    const tab = raw.indexOf("\t");
    const path = raw.slice(tab + 1);
    const parts = raw.slice(0, tab).split(" ");
    if (tab < 0 || parts.length !== 3 || !validPath(path)) throw new Error("invalid Git entry");
    if (!head && parts[2] !== "0") throw new Error(`unmerged index entry: ${path}`);
    catalog.set(path, [parts[0], parts[head ? 2 : 1]]);
  }
  if (!committed) for (const path of (await git.run(["ls-files", "--others", "--exclude-standard", "-z"])).toString().split("\0")) {
    if (!path || path.endsWith("/")) continue;
    if (!validPath(path)) throw new Error("unsafe repository path");
    if (!catalog.has(path)) catalog.set(path, ["", ""]);
  }
  if (catalog.size > 10_000) throw new Error("repository exceeds 10000 files");
  const result: File[] = [];
  for (const path of [...catalog.keys()].sort(compare)) {
    git.signal.throwIfAborted();
    let [mode, oid] = catalog.get(path)!;
    if (!committed && mode !== "160000") {
      const full = join(git.root, path);
      let info;
      try {
        info = await lstat(full);
        if (await realpath(dirname(full)) !== dirname(full)) throw new Error(`symlinked directory: ${path}`);
      } catch (error) { if ((error as NodeJS.ErrnoException).code === "ENOENT") continue; throw error; }
      if (info.isSymbolicLink()) mode = "120000";
      else if (info.isFile()) mode = (info.mode & 0o111) ? "100755" : "100644";
      else throw new Error(`unsupported entry: ${path}`);
      oid = "";
    }
    if (!committed && mode === "160000") oid = await git.gitlinkOID(path, oid);
    const kind = mode === "160000" ? "gitlink" : mode === "120000" ? "symlink" : ["100644", "100755"].includes(mode) ? "regular" : undefined;
    if (!kind) throw new Error(`unsupported Git mode: ${mode}`);
    const ecosystem = kind === "regular" ? manifestEcosystem(path) : "";
    const role = ecosystem ? "manifest" : kind === "regular" ? fileRole(path) : undefined;
    result.push({ path, kind, mode, ...(oid ? { oid } : {}), ...(role ? { role } : {}), ...(ecosystem ? { manifest: { path, ecosystem } } : {}) });
  }
  return result;
}

function fileRole(path: string): string | undefined {
  const name = posix.basename(path);
  if (["Makefile", "makefile", "GNUmakefile", "CMakeLists.txt", "build.gradle", "build.gradle.kts"].includes(name)) return "build_script";
  if (["go.sum", "uv.lock", "poetry.lock", "Pipfile.lock", "Cargo.lock", "package-lock.json", "npm-shrinkwrap.json", "pnpm-lock.yaml", "yarn.lock", "bun.lock", "bun.lockb"].includes(name)) return "lockfile";
  return undefined;
}
