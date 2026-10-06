import { posix } from "node:path";
import type { Repository, Product } from "@compforge/harness-common";
import type { ComponentBinding, Layout, PackageTool } from "./model.js";
import { language } from "./language.js";

export type Catalog = ReadonlyMap<string, Buffer | null>;
export const compare = (a: string, b: string): number => Buffer.compare(Buffer.from(a), Buffer.from(b));
export const needsContent = (name: string): boolean => name === ".repocli.json" || posix.basename(name) === "package.json";
export const validPath = (name: string): boolean => name !== "" && name !== "." && name !== ".." &&
  !name.includes("\0") && !name.includes("\\") && !name.startsWith("/") && !name.startsWith("../") &&
  posix.normalize(name) === name && !name.endsWith("/");

export function fromOrigin(origin: string): Repository | null {
  let host: string, path: string;
  if (origin.includes("://")) {
    try {
      const url = new URL(origin);
      if (url.protocol === "file:") return null;
      host = url.hostname;
      path = decodeURIComponent(url.pathname).replace(/^\//, "");
    } catch { return null; }
  } else {
    const colon = origin.indexOf(":");
    if (colon < 0) return null;
    host = origin.slice(0, colon).replace(/^[^@]*@/, "");
    path = origin.slice(colon + 1);
  }
  path = path.replace(/\/$/, "").replace(/\.git$/, "");
  if (!host || !path) return null;
  return { forge: { name: host === "github.com" ? "github" : host === "gitlab.com" ? "gitlab" : host }, path };
}

const excluded = new Set(["node_modules", "vendor", "venv", "env", "dist", "build", "target", "__pycache__", "testdata"]);
const skip = (name: string): boolean => name.split("/").slice(0, -1).some(p => p.startsWith(".") || excluded.has(p));
const markers = [["python", "pyproject.toml", "setup.py"], ["go", "go.mod"], ["node", "package.json"]];

function detectLanguage(files: Catalog, root: string): string {
  for (const [ecosystem, ...manifests] of markers) {
    if (ecosystem === "python") manifests.push("requirements.txt");
    for (const manifest of manifests) {
      const name = posix.join(root, manifest);
      if (!files.has(name)) continue;
      if (ecosystem !== "node") return ecosystem;
      const text = files.get(name)?.toString().toLowerCase() ?? "";
      return text.includes("typescript") || text.includes("@types/") || files.has(posix.join(root, "tsconfig.json")) ? "typescript" : "javascript";
    }
  }
  const languages = new Set<string>();
  for (const name of files.keys()) {
    if (skip(name) || root !== "." && !name.startsWith(root + "/")) continue;
    const value = language(name);
    if (value) languages.add(value === "tsx" ? "typescript" : value);
  }
  return languages.size > 1 ? "mixed" : languages.values().next().value ?? "";
}

function discover(files: Catalog): Record<string, unknown>[] {
  const roots = new Set<string>();
  for (const name of files.keys()) {
    if (!skip(name) && markers.some(([, ...names]) => names.includes(posix.basename(name)))) roots.add(posix.dirname(name));
  }
  const selected: string[] = [];
  for (const root of [...roots].sort(compare)) {
    if (!selected.some(parent => parent !== "." && root.startsWith(parent + "/"))) selected.push(root);
  }
  if (!selected.length) selected.push(".");
  return selected.map(root => ({ name: root, root }));
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

// Decode only the shared JSON fields. Null uses Go's zero-value semantics;
// unknown fields remain forward-compatible, while wrong field types are errors.
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
function array(value: unknown): unknown[] {
  if (value == null) return [];
  if (!Array.isArray(value)) throw new Error("expected an array");
  return value;
}
function repository(value: unknown): Repository {
  const obj = object(value);
  return { forge: { name: string(field(object(field(obj, "forge")), "name")) }, path: string(field(obj, "path")) };
}

export function load(files: Catalog, origin: string): Layout {
  let repo = fromOrigin(origin);
  let components: unknown[] = [];
  const data = files.get(".repocli.json");
  if (data) {
    try {
      const config = object(JSON.parse(data.toString()));
      const declared = field(config, "repository");
      if (declared !== undefined) {
        // encoding/json merges a partial repository object into the derived origin.
        if (declared === null) repo = null;
        else {
          const value = object(declared);
          const forge = field(value, "forge");
          repo = {
            forge: { name: string(field(object(forge), "name") ?? repo?.forge.name) },
            path: string(field(value, "path") ?? repo?.path),
          };
        }
      }
      if (repo && (!repo.forge.name || !repo.path)) throw new Error("repository requires forge.name and path");
      components = array(field(config, "components"));
    } catch (error) { throw new Error("read .repocli.json", { cause: error }); }
  }
  if (!components.length) components = discover(files);
  const roots = new Set<string>(), names = new Set<string>();
  const bindings = components.map(value => {
    const item = object(value);
    const root = string(field(item, "root")) || ".";
    const name = string(field(item, "name"));
    if (!name || names.has(name) || roots.has(root) || root !== "." && !validPath(root)) throw new Error("invalid or duplicate component name/root");
    roots.add(root); names.add(name);
    const productNames = new Set<string>();
    const products: Product[] = array(field(item, "products")).map(value => {
      const name = string(field(object(value), "name"));
      if (!name || productNames.has(name)) throw new Error("empty or duplicate product name");
      productNames.add(name);
      return { name };
    }).sort((a, b) => compare(a.name, b.name));
    const description = string(field(item, "description"));
    const language = string(field(item, "language")) || detectLanguage(files, root);
    // Configured tool observations are recomputed, but malformed known fields
    // still reject the config, matching the shared JSON contract.
    for (const value of array(field(item, "packageTools"))) {
      const tool = object(value);
      string(field(tool, "name"));
      string(field(tool, "version"));
      for (const evidence of array(field(tool, "evidence"))) string(evidence);
    }
    const tools = packageTools(files, root);
    // Validate identity even when the top-level identity supplies the final value.
    const identity = repository(field(item, "repository"));
    const binding: ComponentBinding = { repository: repo ?? identity, name, root, products,
      ...(description ? { description } : {}), ...(language ? { language } : {}),
      ...(tools.length ? { packageTools: tools } : {}) };
    return binding;
  }).sort((a, b) => compare(a.root, b.root));
  return { repository: repo, components: bindings };
}
