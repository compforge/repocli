"use strict";
const $ = (id) => document.getElementById(id);
const make = (tag, text, cls) => {
  const e = document.createElement(tag);
  if (text !== undefined) e.textContent = text;
  if (cls) e.className = cls;
  return e;
};
let model,
  nodes = new Map(),
  sourceContributions = new Map(),
  mode = { type: "overview" },
  activeTab = "components",
  cy,
  detailEpoch = 0;
const relationGroups = [
  { label: "Calls", kinds: ["calls"], color: "#3569c8" },
  { label: "References", kinds: ["references", "aliases", "decorates"], color: "#ba7925" },
  { label: "Types", kinds: ["extends", "implements"], color: "#8556b8" },
  { label: "Modules", kinds: ["imports", "exports"], color: "#258476" },
  { label: "Structure", kinds: ["contains", "encloses", "declares", "occurs_in", "in_directory", "in_namespace"], color: "#b65c86" },
  { label: "Other", kinds: [], color: "#788494" },
];
const enabledKinds = new Set();
const enabledNodeKinds = new Set();
let readingMap;
const NODE_LIMIT = 250,
  EDGE_LIMIT = 800;
function notice(message = "") {
  $("notice").textContent = message;
  $("notice").hidden = !message;
}
async function api(path, options) {
  const response = await fetch(path, options);
  if (!response.ok) throw new Error(await response.text());
  return response.json();
}
function accepted(r) {
  return (
    enabledKinds.has(r.kind) &&
    ($("confidence").value === "all" || r.confidence === $("confidence").value)
  );
}
function label(n) {
  return n.path || n.qualifiedName || n.name;
}
// Packages/modules may have several declaring documents and no single location.
function sourcePaths(n) {
  if (!n) return [];
  if (n.location) return [n.location.path];
  return [...(sourceContributions.get(n.id) || [])];
}
function sourceLabel(n) {
  if (n.kind === "Directory") return n.path;
  return n.location ? `${n.location.path}:${n.location.line || 1}` : "Multiple source contributions";
}
function button(text, action) {
  const b = make("button", text);
  b.addEventListener("click", action);
  return b;
}
function setModel(data) {
  model = data;
  nodes = new Map(data.nodes.map((n) => [n.id, n]));
  // Index once per snapshot: document aggregation visits every relationship.
  sourceContributions = new Map();
  for (const r of data.relations) {
    if (r.kind !== "declares") continue;
    if (!sourceContributions.has(r.target)) sourceContributions.set(r.target, new Set());
    sourceContributions.get(r.target).add(r.location.path);
  }
  const repository = data.repository;
  $("repo").textContent = repository
    ? `${repository.forge.name}/${repository.path}`
    : data.snapshot.checkout;
  $("repo").title = data.snapshot.checkout;
  $("component-count").textContent = data.components.length;
  $("snapshot").textContent =
    `Working tree snapshot\n${data.snapshot.snapshot}`;
  $("diagnostic-count").textContent = data.diagnostics.length + data.snapshot.diagnostics.length;
  const kinds = [...new Set(data.relations.map((r) => r.kind))].sort();
  enabledKinds.clear();
  $("kinds").replaceChildren();
  for (const kind of kinds) {
    if (["calls", "references", "extends", "implements"].includes(kind)) enabledKinds.add(kind);
    const row = make("label"),
      input = make("input");
    input.type = "checkbox";
    input.checked = enabledKinds.has(kind);
    input.addEventListener("change", () => {
      if (input.checked) enabledKinds.add(kind);
      else enabledKinds.delete(kind);
      updateReadingMap();
    });
    row.append(input, document.createTextNode(kind));
    $("kinds").append(row);
  }
  const nodeKinds = [...new Set(data.nodes.map((n) => n.kind))].sort();
  enabledNodeKinds.clear();
  $("node-kinds").replaceChildren();
  const defaults = nodeKinds.some((kind) => ["Struct", "Class", "Interface"].includes(kind))
    ? ["Struct", "Class", "Interface"] : ["Function"];
  for (const kind of nodeKinds) {
    const row = make("label"), input = make("input");
    input.type = "checkbox";
    input.checked = defaults.includes(kind);
    if (input.checked) enabledNodeKinds.add(kind);
    input.addEventListener("change", () => {
      if (input.checked) enabledNodeKinds.add(kind);
      else enabledNodeKinds.delete(kind);
      mode = { type: "overview" };
      updateReadingMap();
    });
    row.append(input, document.createTextNode(kind));
    $("node-kinds").append(row);
  }
  mode = { type: "overview" };
  detailEpoch++;
  $("details").replaceChildren(
    make("h2", "Follow the code."),
    make(
      "p",
      "Select a document, symbol or relationship to inspect its source and evidence.",
    ),
  );
  cy?.destroy();
  cy = undefined;
  renderComponents();
  renderDiagnostics();
  updateReadingMap();
  activateTab(activeTab);
  if (!data.snapshot.complete)
    notice("Snapshot has capture gaps. Open Diagnostics for details.");
  else if (data.diagnostics.some((d) => d.code === "document_limit"))
    notice(
      "Graph scope is limited by --max-documents. Open Diagnostics for omitted counts.",
    );
}
function updateReadingMap() {
  if (!model) return;
  readingMap = buildReadingMap(model.nodes, model.relations, enabledNodeKinds, accepted, $("include-supporting").checked);
  if (mode.id && !readingMap.metrics.has(mode.id)) mode = { type: "overview" };
  renderResults();
  renderGraph();
}
function renderResults() {
  if (!readingMap) return;
  const query = $("search").value.trim().toLowerCase();
  const matches = readingMap.ranked.filter((n) => !query ||
    `${label(n)} ${sourcePaths(n).join(" ")}`.toLowerCase().includes(query));
  $("result-count").textContent = `${matches.length}`;
  const list = $("results");
  list.replaceChildren();
  const limit = query ? 100 : 10;
  for (const [index, n] of matches.slice(0, limit).entries()) {
    const b = button("", () => focusNode(n));
    b.className = "result";
    const metric = readingMap.metrics.get(n.id);
    b.append(
      make("span", `${query ? "" : `${index + 1}. `}${n.name || label(n)}`, "name"),
      make("small", `${n.kind} · ${metric.neighbors.size} neighbors · ${metric.incoming.size} in / ${metric.outgoing.size} out`),
      make("small", sourceLabel(n)),
    );
    list.append(b);
  }
  if (matches.length > limit)
    list.append(make("p", query ? "Showing the first 100 matches. Refine your search." : "Top 10 by distinct semantic neighbors. Search to find other nodes."));
  if (!matches.length) list.append(make("p", "No matching nodes in the selected kinds."));
}
function focusNode(n) {
  mode = { type: "symbol", id: n.id };
  activateTab("graph");
  renderGraph();
  showNode(n);
}
// +spec=`The default graph shows the selected scope; Top 10 is an optional reading view`
function renderGraph() {
  if (!model || !readingMap || activeTab !== "graph") return;
  let visible, links, title, total;
  if (mode.type === "overview" || mode.type === "top10") {
    total = readingMap.ranked.length;
    visible = readingMap.ranked.slice(0, mode.type === "top10" ? 10 : NODE_LIMIT);
    title = mode.type === "top10" ? "Top 10 · Most connected nodes" : "Repository graph";
  } else {
    const focus = nodes.get(mode.id);
    const adjacent = new Set([mode.id]);
    for (const r of readingMap.links) {
      if (r.source === mode.id) adjacent.add(r.target);
      if (r.target === mode.id) adjacent.add(r.source);
    }
    const candidates = readingMap.ranked.filter((n) => adjacent.has(n.id));
    total = candidates.length;
    visible = candidates.slice(0, NODE_LIMIT);
    // Keep the selected node visible even when its neighbors outrank it.
    if (enabledNodeKinds.has(focus.kind) && !visible.some((n) => n.id === focus.id))
      visible = [focus, ...visible.slice(0, NODE_LIMIT - 1)];
    title = label(focus);
  }
  const ids = new Set(visible.map((n) => n.id));
  links = readingMap.links.filter((r) => ids.has(r.source) && ids.has(r.target));
  const totalEdges = links.length;
  links = links.slice(0, EDGE_LIMIT);
  $("view-title").textContent = title;
  $("overview").classList.toggle("active", mode.type === "top10");
  $("overview").setAttribute("aria-pressed", String(mode.type === "top10"));
  const limited = (mode.type !== "top10" && visible.length < total) || links.length < totalEdges;
  const description = mode.type === "symbol"
    ? "One-hop neighborhood. Double-click a symbol to follow it."
    : mode.type === "top10"
      ? "Top 10 by distinct semantic neighbors across the selected kinds."
      : "Global view of the selected kinds. Size shows connectivity; click a node to explore.";
  $("scope").textContent =
    `${visible.length} / ${total} nodes · ${links.length} / ${totalEdges} displayed relationships${limited ? " · Display limit reached. Narrow the selected kinds or focus a node." : ""} · ${description}`;
  $("graph-empty").hidden = visible.length !== 0;
  if (cy) cy.destroy();
  cy = cytoscape({
    container: $("graph"),
    elements: [
      ...visible.map((n) => ({
        data: {
          id: n.id,
          label: n.kind === "Document" ? n.location.path : `${n.name}\n${(n.location?.path || "").split("/").slice(-2).join("/")}`,
          kind: n.kind,
          size: 28 + 10 * Math.log2(1 + (readingMap.metrics.get(n.id)?.neighbors.size || 0)),
        },
        classes: n.id === mode.id ? "focus" : "",
      })),
      ...links.map((r) => ({
        data: {
          id: r.id,
          source: r.source,
          target: r.target,
          label:
            r.kind + (r.evidence.length > 1 ? ` · ${r.evidence.length}` : ""),
          evidence: r.evidence,
          color: (relationGroups.find((group) => group.kinds.includes(r.kind)) || relationGroups.at(-1)).color,
        },
        classes: r.evidence.some((e) => e.confidence !== "exact")
          ? "uncertain"
          : "",
      })),
    ],
    style: [
      {
        selector: "node",
        style: {
          shape: "hexagon",
          "background-color": "#6486c4",
          label: "data(label)",
          color: "#334562",
          "font-size": 11,
          "min-zoomed-font-size": 7,
          "text-valign": "bottom",
          "text-margin-y": 8,
          "text-wrap": "wrap",
          "text-max-width": 150,
          width: "data(size)",
          height: "data(size)",
          "border-width": 2,
          "border-color": "#fff",
        },
      },
      {
        selector:
          'node[kind = "Class"], node[kind = "Struct"], node[kind = "Interface"]',
        style: {
          shape: "diamond",
          "background-color": "#309483",
          width: "data(size)",
          height: "data(size)",
        },
      },
      {
        selector: 'node[kind = "Function"], node[kind = "Method"]',
        style: { shape: "ellipse" },
      },
      {
        selector: 'node[kind = "Field"]',
        style: { shape: "rectangle" },
      },
      {
        selector: "node.focus",
        style: {
          "border-width": 5,
          "border-color": "#b0c8fa",
          "background-color": "#234fb0",
        },
      },
      {
        selector: "edge",
        style: {
          width: 1.3,
          "line-color": "data(color)",
          "target-arrow-color": "data(color)",
          "line-style": "solid",
          "target-arrow-shape": "triangle",
          "curve-style": "bezier",
          label: links.length > 80 ? "" : "data(label)",
          "font-size": 9,
          color: "#687b96",
          "text-background-color": "#f5f7fb",
          "text-background-opacity": 0.9,
          "text-background-padding": "2px",
          "arrow-scale": 0.8,
        },
      },
      {
        selector: "edge.uncertain",
        style: {
          "line-style": "dashed",
        },
      },
      { selector: ".dimmed", style: { opacity: 0.12 } },
      { selector: "edge:selected", style: { label: "data(label)", width: 3 } },
      {
        selector: "node:selected",
        style: {
          "background-color": "#d38639",
        },
      },
    ],
    layout: {
      name: visible.length > 100 ? "grid" : "cose",
      animate: false,
      randomize: true,
      padding: 45,
      nodeRepulsion: 18000,
      idealEdgeLength: 110,
    },
    minZoom: 0.12,
    maxZoom: 3,
  });
  cy.on("tap", "node", (event) => {
    const n = nodes.get(event.target.id());
    showNode(n);
  });
  cy.on("dbltap", "node", (event) => focusNode(nodes.get(event.target.id())));
  cy.on("tap", "edge", (event) => showRelations(event.target.data("evidence")));
  cy.on("mouseover", "node", (event) => {
    cy.elements().addClass("dimmed");
    event.target.closedNeighborhood().removeClass("dimmed");
  });
  cy.on("mouseout", "node", () => cy.elements().removeClass("dimmed"));
}
function componentFor(path) {
  return model.components.reduce((owner, component) => {
    const root = component.root;
    const contains =
      root === "." || path === root || path.startsWith(root + "/");
    return contains &&
      (!owner || owner.root === "." || root.length > owner.root.length)
      ? component
      : owner;
  }, undefined);
}
// +spec=`Component, graph and diagnostics have separate panels sharing one snapshot`
function activateTab(name) {
  activeTab = name;
  if (model) {
    $("stats").textContent = name === "components"
      ? `${model.components.length} components`
      : name === "diagnostics"
        ? `${model.snapshot.diagnostics.length} capture gaps · ${model.diagnostics.length} graph diagnostics`
        : `${model.documents} documents · ${model.nodes.length} nodes · ${model.relations.length} relations`;
  }
  for (const tab of document.querySelectorAll('[role="tab"]')) {
    const selected = tab.id === `tab-${name}`;
    tab.setAttribute("aria-selected", String(selected));
    tab.tabIndex = selected ? 0 : -1;
    $(tab.getAttribute("aria-controls")).hidden = !selected;
  }
  if (name === "graph") {
    if (cy) cy.resize();
    else renderGraph();
  }
}
// +spec=`Each component is nested under its nearest containing component root`
function renderComponents() {
  const panel = $("component-list");
  panel.replaceChildren();
  if (!model.components.length) {
    panel.append(make("li", "No components identified in this snapshot."));
    return;
  }
  const entries = new Map();
  const components = [...model.components].sort((a, b) => a.root.localeCompare(b.root));
  for (const component of components) {
    const row = make("li", undefined, "component-node");
    const item = make("article", undefined, "component-card");
    item.append(
      make("h2", component.name === "." ? "Repository root" : component.name),
      make("span", component.language || "Unknown language", "badge"),
    );
    const metadata = make("div", undefined, "component-metadata");
    const root = make("div"), manifests = make("div"), tools = make("div");
    root.append(make("h3", "Root directory"), make("p", component.root, "meta"));
    manifests.append(make("h3", "Manifests"));
    for (const manifest of component.manifests || [])
      manifests.append(make("p", manifest, "meta"));
    if (!component.manifests?.length)
      manifests.append(make("p", "Identified by Makefile."));
    tools.append(make("h3", "Package tools"));
    for (const tool of component.packageTools || []) {
      tools.append(
        make("p", `${tool.name}${tool.version ? "@" + tool.version : ""}`, "meta"),
        make("p", tool.evidence.join(", "), "meta"),
      );
    }
    if (!component.packageTools?.length)
      tools.append(make("p", "No package tool identified."));
    metadata.append(root, manifests, tools);
    item.append(metadata);
    const children = make("ul", undefined, "component-children");
    children.hidden = true;
    row.append(item, children);
    entries.set(component.root, { row, children });
  }
  for (const component of components) {
    const parent = components.reduce((nearest, candidate) => {
      if (candidate.root === component.root) return nearest;
      const contains = candidate.root === "." || component.root.startsWith(candidate.root + "/");
      return contains && (!nearest || nearest.root === "." || candidate.root.length > nearest.root.length) ? candidate : nearest;
    }, undefined);
    const target = parent ? entries.get(parent.root).children : panel;
    target.hidden = false;
    target.append(entries.get(component.root).row);
  }
}
function showNode(n) {
  const epoch = ++detailEpoch,
    panel = $("details");
  panel.replaceChildren();
  panel.append(
    make("span", n.kind, "badge"),
    make("h2", label(n)),
    make("p", sourceLabel(n), "meta"),
  );
  const path = n.path || n.location?.path;
  const component = path ? componentFor(path) : undefined;
  if (n.tags?.length) panel.append(make("p", `Tags: ${n.tags.join(", ")}`, "meta"));
  panel.append(
    make(
      "p",
      component
        ? `Component: ${component.name} · ${component.language || "unknown language"}`
        : "No owning component",
      "meta",
    ),
  );
  const metric = readingMap.metrics.get(n.id);
  if (metric) {
    panel.append(make("p", `${metric.neighbors.size} distinct semantic neighbors · ${metric.incoming.size} incoming · ${metric.outgoing.size} outgoing`, "meta"));
    panel.append(button("Explore this neighborhood", () => focusNode(n)));
  }
  for (const doc of (n.documentation || []).slice(0, 2))
    panel.append(make("p", doc.text));
  for (const marker of n.markers || [])
    panel.append(make("div", `${marker.kind}: ${marker.text}`, "item"));
  const incoming = model.relations.filter((r) => r.target === n.id),
    outgoing = model.relations.filter((r) => r.source === n.id);
  panel.append(
    make("h3", `${incoming.length} incoming · ${outgoing.length} outgoing`),
  );
  panel.append(
    button("Inspect relationships", () =>
      showRelations([...incoming, ...outgoing]),
    ),
  );
  if (n.kind === "Directory") {
    panel.append(make("h3", "Captured children"));
    for (const r of incoming.filter((r) => r.kind === "in_directory")) {
      const child = nodes.get(r.source);
      if (child) panel.append(button(label(child), () => focusNode(child)));
    }
    return;
  }
  if (!n.location) {
    panel.append(make("h3", "Contributing documents"));
    for (const path of sourcePaths(n)) {
      const doc = model.nodes.find((node) => node.kind === "Document" && node.location.path === path);
      if (doc) panel.append(button(path, () => focusNode(doc)));
    }
    return;
  }
  const issues = model.diagnostics.filter(
    (d) => d.location.path === n.location.path,
  );
  if (issues.length)
    panel.append(
      button(`${issues.length} document diagnostics`, () =>
        showDiagnostics(n.location.path),
      ),
    );
  panel.append(make("h3", "Captured source"));
  const pre = make("pre", "Loading…", "source");
  panel.append(pre);
  const requestedSnapshot = model.snapshot.snapshot;
  api(
    "/api/source?" +
      new URLSearchParams({
        snapshot: requestedSnapshot,
        path: n.location.path,
        from: String(Math.max(1, (n.location.line || 1) - 5)),
      }),
  )
    .then((result) => {
      if (epoch !== detailEpoch) return;
      pre.replaceChildren();
      result.lines.forEach((line, index) => {
        const number = result.from + index,
          row = make("span", undefined, "line");
        if (
          n.kind !== "Document" &&
          number >= n.location.line &&
          number <= n.location.endLine
        )
          row.classList.add("selected");
        row.append(make("b", String(number)), document.createTextNode(line));
        pre.append(row);
      });
      if (result.from > 1 || result.lines.length < result.total)
        panel.append(
          make(
            "p",
            `Lines ${result.from}–${result.from + result.lines.length - 1} of ${result.total}. Source is from this graph's snapshot.`,
          ),
        );
    })
    .catch((err) => {
      if (epoch === detailEpoch) pre.textContent = err.message;
    });
}
function showRelations(relations) {
  ++detailEpoch;
  const panel = $("details");
  panel.replaceChildren(
    make("h2", "Relationship evidence"),
    make(
      "p",
      `${relations.length} relations. Confidence is evidence strength, not probability.`,
    ),
  );
  for (const r of relations.slice(0, 100)) {
    const item = make("div", undefined, "item");
    item.append(
      make("span", r.kind, "badge"),
      make("span", r.confidence, `badge ${r.confidence}`),
    );
    for (const [index, id] of [r.source, r.target].entries()) {
      const n = nodes.get(id);
      if (n) item.append(button(label(n), () => showNode(n)));
      if (index === 0) item.append(make("span", " → "));
    }
    item.append(
      make("p", r.location.path ? `${r.location.path}:${r.location.line}` : "Path structure", "meta"),
    );
    for (const proof of r.evidence) {
      const support = proof.location ? ` · ${proof.location.path}:${proof.location.line}` : "";
      item.append(make("p", `${proof.basis} · ${proof.confidence}${support}`, "meta"));
    }
    panel.append(item);
  }
  if (relations.length > 100)
    panel.append(
      make(
        "p",
        "Showing the first 100 relations. Focus a symbol for a smaller set.",
      ),
    );
}
function showDiagnostics(path = "") {
  $("diagnostic-search").value = path;
  renderDiagnostics();
  activateTab("diagnostics");
}
function renderDiagnostics() {
  if (!model) return;
  const panel = $("diagnostic-list");
  panel.replaceChildren();
  const query = $("diagnostic-search").value.trim().toLowerCase();
  const issues = [...model.snapshot.diagnostics, ...model.diagnostics].filter(
    (d) => `${d.location?.path || ""} ${d.code} ${d.message}`.toLowerCase().includes(query),
  );
  panel.append(
    make(
      "p",
      `${issues.length} recorded gaps. An empty list does not prove complete static analysis.`,
    ),
  );
  for (const d of issues.slice(0, 200)) {
    const item = make("div", undefined, "item");
    item.append(make("span", d.code, "badge"), make("p", d.message));
    if (d.location?.path)
      item.append(
        make(
          "p",
          `${d.location.path}:${d.location.line} · ${d.subject}${d.relation ? " · " + d.relation : ""}`,
          "meta",
        ),
      );
    if (d.outline) item.append(make("p", JSON.stringify(d.outline), "meta"));
    panel.append(item);
  }
  if (issues.length > 200)
    panel.append(
      make(
        "p",
        "Showing the first 200 diagnostics. Refine your search to narrow the list.",
      ),
    );
}
$("search").addEventListener("input", renderResults);
$("confidence").addEventListener("change", updateReadingMap);
$("include-supporting").addEventListener("change", updateReadingMap);
$("overview").addEventListener("click", () => {
  mode = { type: mode.type === "top10" ? "overview" : "top10" };
  renderGraph();
});
$("fit").addEventListener("click", () => cy?.fit(undefined, 45));
$("reset-graph").addEventListener("click", () => {
  if (!model) return;
  $("search").value = "";
  $("confidence").value = "all";
  $("include-supporting").checked = false;
  for (const filter of document.querySelectorAll(".graph-controls details")) filter.open = false;
  setModel(model);
});
$("diagnostic-search").addEventListener("input", renderDiagnostics);
const tabs = [...document.querySelectorAll('[role="tab"]')];
for (const [index, tab] of tabs.entries()) {
  tab.addEventListener("click", () => activateTab(tab.id.slice(4)));
  tab.addEventListener("keydown", (event) => {
    let next;
    if (event.key === "ArrowRight") next = (index + 1) % tabs.length;
    else if (event.key === "ArrowLeft") next = (index + tabs.length - 1) % tabs.length;
    else if (event.key === "Home") next = 0;
    else if (event.key === "End") next = tabs.length - 1;
    else return;
    event.preventDefault();
    tabs[next].focus();
    activateTab(tabs[next].id.slice(4));
  });
}
$("refresh").addEventListener("click", async () => {
  $("refresh").disabled = true;
  $("refresh").textContent = "Building…";
  notice("Capturing a new snapshot. The current snapshot remains available.");
  try {
    const data = await api("/api/refresh", {
      method: "POST",
      headers: { "X-Repocli-View": "1" },
    });
    notice();
    setModel(data);
  } catch (err) {
    notice(`Refresh failed; keeping the previous snapshot. ${err.message}`);
  } finally {
    $("refresh").disabled = false;
    $("refresh").textContent = "↻ Refresh snapshot";
  }
});
for (const group of relationGroups) {
  const item = make("span"), swatch = make("i");
  item.title = group.kinds.length ? group.kinds.join(", ") : "Other relation kinds";
  swatch.style.backgroundColor = group.color;
  swatch.setAttribute("aria-hidden", "true");
  item.append(swatch, document.createTextNode(group.label));
  $("relation-legend").append(item);
}
api("/api/graph")
  .then(setModel)
  .catch((err) => notice(`Cannot load graph: ${err.message}`));
