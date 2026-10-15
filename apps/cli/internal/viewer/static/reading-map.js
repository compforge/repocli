"use strict";

// +spec=`Reading rank counts distinct semantic neighbors, not reference occurrences or ownership edges`
// +why=`Centrality provides an explainable reading order, not a claim of business importance`
function buildReadingMap(allNodes, relations, selectedKinds, acceptRelation, includeSupporting = false) {
  const byID = new Map(allNodes.map((n) => [n.id, n]));
  const documentTags = new Map(allNodes.filter((n) => n.kind === "Document").map((n) => [n.location?.path, n.tags || []]));
  function excluded(n) {
    if (includeSupporting) return false;
    const path = n?.location?.path || n?.path || "";
    return /_test\.go$|(^|\/)(tests?|__tests__)\//.test(path) ||
      (documentTags.get(path) || []).some((tag) => ["dependency", "generated", "test_fixture", "build_output"].includes(tag));
  }
  const selected = new Map(allNodes.filter((n) => selectedKinds.has(n.kind) && !excluded(n)).map((n) => [n.id, n]));
  const documents = new Map(allNodes.filter((n) => n.kind === "Document").map((n) => [n.location?.path, n.id]));
  const parents = new Map();
  // Semantic membership wins over lexical enclosure, including Go receivers.
  for (const kind of ["encloses", "contains", "occurs_in"]) {
    for (const r of relations) {
      if (r.kind !== kind) continue;
      if (kind === "occurs_in") parents.set(r.source, r.target);
      else parents.set(r.target, r.source);
    }
  }
  const owners = new Map();
  function owner(id) {
    if (owners.has(id)) return owners.get(id);
    let current = id;
    const seen = new Set();
    while (!selected.has(current) && parents.has(current) && !seen.has(current)) {
      seen.add(current);
      current = parents.get(current);
    }
    const doc = documents.get(byID.get(id)?.location?.path);
    const result = selected.has(current) ? current : selected.has(doc) ? doc : undefined;
    owners.set(id, result);
    return result;
  }
  const metrics = new Map([...selected.keys()].map((id) => [id, {
    neighbors: new Set(), incoming: new Set(), outgoing: new Set(),
  }]));
  const groups = new Map();
  const semantic = new Set(["calls", "references", "extends", "implements", "aliases", "imports", "exports", "decorates"]);
  for (const r of relations) {
    if (!acceptRelation(r) || excluded(byID.get(r.source)) || excluded(byID.get(r.target))) continue;
    const source = owner(r.source), target = owner(r.target);
    if (!source || !target || source === target) continue;
    if (semantic.has(r.kind)) {
      metrics.get(source).neighbors.add(target);
      metrics.get(target).neighbors.add(source);
      metrics.get(source).outgoing.add(target);
      metrics.get(target).incoming.add(source);
    }
    const key = JSON.stringify([source, target, r.kind]);
    if (!groups.has(key)) groups.set(key, { id: `reading-link-${groups.size}`, source, target, kind: r.kind, evidence: [] });
    groups.get(key).evidence.push(r);
  }
  const ranked = [...selected.values()].sort((a, b) =>
    metrics.get(b.id).neighbors.size - metrics.get(a.id).neighbors.size ||
    `${a.location?.path || a.path}:${a.name}`.localeCompare(`${b.location?.path || b.path}:${b.name}`));
  return { ranked, metrics, links: [...groups.values()] };
}
