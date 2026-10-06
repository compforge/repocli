import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { test } from "node:test";
import { load } from "../dist/layout.js";
import { language } from "../dist/language.js";
import { owner } from "../dist/index.js";

const cases = JSON.parse(readFileSync(new URL("../../../conformance/inspect/layouts.json", import.meta.url)));
for (const item of cases) {
  test(`shared layout: ${item.name}`, () => {
    const files = new Map(Object.entries(item.files).map(([name, text]) => [name, Buffer.from(text)]));
    if (item.error) { assert.throws(() => load(files, item.origin)); return; }
    const layout = load(files, item.origin);
    assert.deepEqual(layout, item.expected);
    for (const [path, name] of Object.entries(item.owners)) assert.equal(owner(layout, path)?.name ?? null, name);
  });
}
test("all generated filename probes agree with pinned Go CodeGraph", () => {
  const catalog = JSON.parse(readFileSync(new URL("../../../conformance/inspect/languages.json", import.meta.url)));
  for (const [path, expected] of Object.entries(catalog)) assert.equal(language(path), expected, path);
});
