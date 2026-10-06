import { posix } from "node:path";
import catalog from "./languages.json" with { type: "json" };

/** Generated from pinned CodeGraph/gotreesitter metadata; no parser is loaded. */
export function language(path: string): string {
  const base = posix.basename(path);
  const ext = base.includes(".") ? base.slice(base.lastIndexOf(".")).toLowerCase() : "";
  const lookup = (table: Record<string, string>, key: string): string | undefined => Object.hasOwn(table, key) ? table[key] : undefined;
  const override = lookup(catalog.overrides, ext);
  if (override !== undefined) return override;
  const filename = lookup(catalog.filenames, base);
  if (filename !== undefined) return filename;
  const suffixes: string[] = [];
  for (let i = base.length - 1; i > 0 && suffixes.length < 4; i--) {
    if (base[i] === ".") suffixes.push(base.slice(i));
  }
  for (const suffix of suffixes.reverse()) {
    const found = lookup(catalog.registry, suffix);
    if (found !== undefined) return found;
  }
  // Go path.Ext includes a leading dot in a dotfile, unlike Node path.extname.
  const goExt = base.includes(".") ? base.slice(base.lastIndexOf(".")).toLowerCase() : "";
  return lookup(catalog.fallback, goExt) ?? "";
}
