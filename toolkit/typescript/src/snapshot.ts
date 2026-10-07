import { createHash } from "node:crypto";
import { constants } from "node:fs";
import { lstat, open, readlink, realpath } from "node:fs/promises";
import { dirname, join, posix } from "node:path";
import { Git } from "./git.js";

export interface Snapshot {
  readonly checkout: string;
  readonly digest: string;
  readonly fileCount: number;
  readonly complete: boolean;
  readonly diagnostics: readonly string[];
}

/** Working-tree input, compatible with Go's complete content digest. */
export async function snapshot(repository: string, options: { timeoutMs?: number; signal?: AbortSignal } = {}): Promise<Snapshot> {
  const timeout = AbortSignal.timeout(options.timeoutMs ?? 30_000);
  const signal = options.signal ? AbortSignal.any([timeout, options.signal]) : timeout;
  const root = await realpath((await new Git(repository, signal).run(["rev-parse", "--show-toplevel"])).toString().replace(/\n$/, ""));
  const first = await capture(root, signal);
  const second = await capture(root, signal);
  return first.digest === second.digest ? second : { ...second, complete: false, diagnostics: [...second.diagnostics, "snapshot_changed"] };
}

function frame(name: string, data: Buffer): Buffer {
  return Buffer.concat([Buffer.from(`${Buffer.byteLength(name)}:${name}${data.length}:`), data]);
}
function sorted(names: Iterable<string>): string[] { return [...names].sort((a, b) => Buffer.compare(Buffer.from(a), Buffer.from(b))); }

async function capture(root: string, signal: AbortSignal): Promise<Snapshot> {
  const git = new Git(root, signal);
  const entries = new Map<string, readonly [string, string]>();
  for (const record of (await git.run(["ls-files", "--stage", "-z"])).toString().split("\0")) {
    if (!record) continue;
    const tab = record.indexOf("\t");
    const [mode, oid, stage] = record.slice(0, tab).split(" ");
    if (tab < 0 || stage !== "0") throw new Error("unmerged or invalid index");
    entries.set(record.slice(tab + 1), [mode, oid]);
  }
  for (const name of (await git.run(["ls-files", "--others", "--exclude-standard", "-z"])).toString().split("\0")) {
    if (name && !name.endsWith("/") && !entries.has(name)) entries.set(name, ["", ""]);
  }
  if (entries.size > 10_000) throw new Error("repository exceeds 10000 files");
  const digest = createHash("sha256").update("repocli-snapshot-v2\0");
  const files = new Set<string>();
  const links = new Map<string, string>();
  const modules = new Map<string, string>();
  const large = new Map<string, string>();
  const issues: string[] = [];
  let total = 0;
  for (const name of sorted(entries.keys())) {
    signal.throwIfAborted();
    const path = join(root, name);
    if (entries.get(name)![0] === "160000") {
      modules.set(name, await git.gitlinkOID(name, entries.get(name)![1]));
      continue;
    }
    let info;
    try {
      info = await lstat(path);
      if (await realpath(dirname(path)) !== dirname(path)) { issues.push(`${name}: symlinked parent`); continue; }
    } catch (error) { if ((error as NodeJS.ErrnoException).code === "ENOENT") continue; throw error; }
    if (info.isSymbolicLink()) links.set(name, await readlink(path));
    else if (info.isFile()) {
      const file = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK);
      try {
        if (!(await file.stat()).isFile()) throw new Error("file changed kind during snapshot");
        const chunks: Buffer[] = [];
        const h = createHash("sha256");
        const buffer = Buffer.alloc(65536);
        let size = 0;
        while (true) {
          signal.throwIfAborted();
          const { bytesRead } = await file.read(buffer, 0, buffer.length, null);
          if (!bytesRead) break;
          size += bytesRead;
          if (info.size > 2 << 20) h.update(buffer.subarray(0, bytesRead));
          else {
            if (size > 2 << 20) throw new Error("file grew during snapshot");
            chunks.push(Buffer.from(buffer.subarray(0, bytesRead)));
          }
        }
        if (info.size > 2 << 20) large.set(name, `${size}:sha256:${h.digest("hex")}`);
        else {
          total += size;
          if (total > 128 << 20) throw new Error("snapshot exceeds 128 MiB");
          digest.update(frame(name, Buffer.concat(chunks)));
        }
        files.add(name);
      } finally { await file.close(); }
    } else issues.push(`${name}: unsupported file kind`);
  }
  for (const name of links.keys()) {
    let current = name;
    const seen = new Set<string>();
    while (links.has(current)) {
      const target = links.get(current)!;
      if (seen.has(current) || posix.isAbsolute(target)) { current = ""; break; }
      seen.add(current);
      current = posix.normalize(posix.join(posix.dirname(current), target));
      if (current === ".." || current.startsWith("../")) { current = ""; break; }
    }
    const captured = files.has(current) || modules.has(current)
      || current !== "" && [...files].some(f => current === "." || f.startsWith(current + "/"));
    if (!captured) issues.push(`${name}: symlink target is outside captured contents or cyclic/missing`);
  }
  for (const [kind, values] of [["symlink", links], ["gitlink", modules], ["large_file", large]] as const) {
    for (const name of sorted(values.keys())) digest.update(Buffer.concat([Buffer.from(kind + ":"), frame(name, Buffer.from(values.get(name)!))]));
  }
  for (const issue of sorted(issues)) digest.update(`issue:${Buffer.byteLength(issue)}:${issue}`);
  return { checkout: root, digest: `sha256:${digest.digest("hex")}`, fileCount: files.size, complete: issues.length === 0, diagnostics: issues };
}
