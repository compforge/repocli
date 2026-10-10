import { parseRemoteUrl } from "./remote.js";
import { posix } from "node:path";
import type { Repository } from "@compforge/harness-common";
import type { ComponentBinding, Layout, PackageTool } from "./model.js";

export type Catalog = ReadonlyMap<string, Buffer | null>;
export const compare = (a: string, b: string): number => Buffer.compare(Buffer.from(a), Buffer.from(b));
export const needsContent = (name: string): boolean => posix.basename(name) === "package.json";
export const validPath = (name: string): boolean => name !== "" && name !== "." && name !== ".." &&
  !name.includes("\0") && !name.includes("\\") && !name.startsWith("/") && !name.startsWith("../") &&
  posix.normalize(name) === name && !name.endsWith("/");

export function fromOrigin(origin: string): Repository | null {
  const remote = parseRemoteUrl(origin);
  if (!remote) return null;
  const { host, path } = remote;
  return { forge: { name: host === "github.com" ? "github" : host === "gitlab.com" ? "gitlab" : host }, path };
}

const excluded = new Set(["node_modules", "vendor", "venv", "env", "dist", "build", "target", "__pycache__", "testdata"]);
const skip = (name: string): boolean => name.split("/").slice(0, -1).some(p => p.startsWith(".") || excluded.has(p));
const markers = [["python", "pyproject.toml", "setup.py"], ["go", "go.mod"], ["node", "package.json"], ["rust", "Cargo.toml"]];

function detectLanguage(files: Catalog, root: string): string {
  for (const [ecosystem, ...manifests] of markers) {
    for (const manifest of manifests) {
      const name = posix.join(root, manifest);
      if (!files.has(name)) continue;
      if (ecosystem !== "node") return ecosystem;
      const text = files.get(name)?.toString().toLowerCase() ?? "";
      return text.includes("typescript") || text.includes("@types/") || files.has(posix.join(root, "tsconfig.json")) ? "typescript" : "javascript";
    }
  }
  return "";
}

function discover(files: Catalog): string[] {
  const candidates = new Map<string, boolean>();
  for (const name of files.keys()) {
    if (skip(name)) continue;
    const root = posix.dirname(name);
    if (manifestEcosystem(name)) candidates.set(root, true);
    else if (posix.basename(name) === "Makefile" && !candidates.has(root)) candidates.set(root, false);
  }
  const selected: string[] = [];
  for (const root of [...candidates.keys()].sort(compare)) {
    // Makefile orchestration must not hide child project boundaries.
    if (!selected.some(parent => candidates.get(parent) && parent !== "." && root.startsWith(parent + "/"))) selected.push(root);
  }
  return selected;
}

function packageTools(files: Catalog, root: string): PackageTool[] {
  const found = new Map<string, { name: string; version?: string; evidence: string[] }>();
  const add = (name: string, version: string, evidence: string) => {
    const tool = found.get(name) ?? { name, evidence: [] };
    if (version) tool.version = version;
    tool.evidence.push(evidence);
    found.set(name, tool);
  };
  const manifest = posix.join(root, "package.json");
  const data = files.get(manifest);
  if (data) {
    // A malformed manifest remains a boundary; independent lockfile evidence survives.
    try {
      const metadata = object(JSON.parse(data.toString()));
      const manager = string(field(metadata, "packageManager"));
      const at = manager.indexOf("@");
      const name = at < 0 ? manager : manager.slice(0, at);
      if (["npm", "pnpm", "yarn", "bun"].includes(name)) add(name, at < 0 ? "" : manager.slice(at + 1), manifest + "#packageManager");
    } catch { /* Best effort, as in Go. */ }
  }
  for (const [file, tool] of [
    ["go.mod", "go"], ["uv.lock", "uv"], ["poetry.lock", "poetry"], ["Pipfile.lock", "pipenv"],
    ["package-lock.json", "npm"], ["npm-shrinkwrap.json", "npm"], ["pnpm-lock.yaml", "pnpm"],
    ["yarn.lock", "yarn"], ["bun.lock", "bun"], ["bun.lockb", "bun"],
  ]) {
    const name = posix.join(root, file);
    if (files.has(name)) add(tool, "", name);
  }
  return [...found.values()].sort((a, b) => compare(a.name, b.name)).map(tool => ({ ...tool, evidence: tool.evidence.sort(compare) }));
}

// Decode package-manager evidence using the shared JSON field semantics.
function object(value: unknown): Record<string, unknown> {
  if (value == null) return {};
  if (typeof value !== "object" || Array.isArray(value)) throw new Error("expected an object");
  return value as Record<string, unknown>;
}
function field(value: Record<string, unknown>, name: string): unknown {
  return Object.entries(value).filter(([key]) => key.toLowerCase() === name.toLowerCase()).at(-1)?.[1];
}
function string(value: unknown): string {
  if (value == null) return "";
  if (typeof value !== "string") throw new Error("expected a string");
  return value;
}
export function load(files: Catalog, origin: string): Layout {
  const repo = fromOrigin(origin);
  const bindings = discover(files).map(root => {
    const language = detectLanguage(files, root);
    const tools = packageTools(files, root);
    const manifests = [...files.keys()].filter(name => posix.dirname(name) === root && manifestEcosystem(name)).sort(compare);
    const binding: ComponentBinding = {
      repository: repo ?? { forge: { name: "" }, path: "" }, name: root, root, products: [],
      ...(language ? { language } : {}), ...(manifests.length ? { manifests } : {}),
      ...(tools.length ? { packageTools: tools } : {}),
    };
    return binding;
  });
  return { repository: repo, components: bindings };
}

export function manifestEcosystem(path: string): string { return markers.find(([, ...names]) => names.includes(posix.basename(path)))?.[0] ?? ""; }
