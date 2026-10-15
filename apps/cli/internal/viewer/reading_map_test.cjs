const { test } = require("node:test");
const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { runInNewContext } = require("node:vm");
const { join } = require("node:path");
const build = runInNewContext(readFileSync(join(__dirname, "static/reading-map.js"), "utf8") + "\n buildReadingMap;");
const node = (id, kind = "Struct", path = "app.go") => ({ id, name: id, kind, location: { path } });
const edge = (source, target, kind = "references") => ({ source, target, kind, confidence: "exact" });
const kinds = new Set(["Struct"]);

test("semantic neighbors collapse members and occurrences without inflating degree", () => {
  const nodes = [node("A"), node("B"), node("C"), node("method", "Method"), node("use", "Reference")];
  const relations = [edge("A", "method", "contains"), edge("use", "method", "occurs_in"),
    edge("method", "B"), edge("use", "B"), edge("method", "A"), edge("B", "C"), edge("A", "C", "contains")];
  const result = build(nodes, relations, kinds, () => true);
  assert.equal(result.ranked[0].id, "B");
  assert.equal(result.metrics.get("A").neighbors.size, 1);
  assert.equal(result.metrics.get("B").neighbors.size, 2);
  assert.equal(result.links.find(r => r.source === "A" && r.target === "B").evidence.length, 2);
  assert.equal(result.links.some(r => r.source === r.target), false);
});

test("relation filters preserve ownership; kind filters change the projection", () => {
  const nodes = [node("A"), node("B"), node("m", "Method")];
  const relations = [edge("A", "m", "contains"), edge("m", "B", "calls")];
  const selected = build(nodes, relations, kinds, r => r.kind === "calls");
  assert.equal(selected.metrics.get("A").outgoing.size, 1);
  const empty = build(nodes, relations, kinds, r => r.kind === "references");
  assert.equal(empty.metrics.get("A").neighbors.size, 0);
  const methods = build(nodes, relations, new Set(["Method", "Struct"]), () => true);
  assert.equal(methods.metrics.get("m").neighbors.size, 1);
  assert.equal(methods.metrics.get("A").neighbors.size, 0);
});

test("supporting code is opt-in and ownership cycles terminate", () => {
  const nodes = [node("A"), node("T", "Struct", "app_test.go"), node("r", "Reference")];
  const relations = [edge("T", "A"), edge("r", "r", "occurs_in"), edge("r", "A")];
  const result = build(nodes, relations, kinds, () => true);
  assert.equal(result.ranked.length, 1);
  assert.equal(result.metrics.get("A").neighbors.size, 0);
  const full = build(nodes, relations, kinds, () => true, true);
  assert.equal(full.metrics.get("A").neighbors.size, 1);
});
