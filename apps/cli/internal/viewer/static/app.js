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
  mode = { type: "documents" },
  cy,
  detailEpoch = 0;
const enabledKinds = new Set();
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
  $("components").textContent =
    `Components · ${data.components.length}`;
  $("stats").textContent =
    `${data.documents} documents · ${data.nodes.length} nodes · ${data.relations.length} relations`;
  $("snapshot").textContent =
    `Working tree snapshot\n${data.snapshot.snapshot}`;
  $("diagnostics").textContent =
    `Diagnostics · ${data.diagnostics.length + data.snapshot.diagnostics.length}`;
  const kinds = [...new Set(data.relations.map((r) => r.kind))].sort();
  enabledKinds.clear();
  $("kinds").replaceChildren();
  for (const kind of kinds) {
    enabledKinds.add(kind);
    const row = make("label"),
      input = make("input");
    input.type = "checkbox";
    input.checked = true;
    input.addEventListener("change", () => {
      if (input.checked) enabledKinds.add(kind);
      else enabledKinds.delete(kind);
      renderGraph();
    });
    row.append(input, document.createTextNode(kind));
    $("kinds").append(row);
  }
  mode = { type: "documents" };
  detailEpoch++;
  $("details").replaceChildren(
    make("h2", "Follow the code."),
    make(
      "p",
      "Select a document, symbol or relationship to inspect its source and evidence.",
    ),
  );
  renderResults();
  renderGraph();
  if (!data.snapshot.complete)
    notice("Snapshot has capture gaps. Open Diagnostics for details.");
  else if (data.diagnostics.some((d) => d.code === "document_limit"))
    notice(
      "Graph scope is limited by --max-documents. Open Diagnostics for omitted counts.",
    );
}
function renderResults() {
  if (!model) return;
  const query = $("search").value.trim().toLowerCase();
  const matches = model.nodes.filter((n) =>
    query
      ? `${label(n)} ${sourcePaths(n).join(" ")}`.toLowerCase().includes(query)
      : n.kind === "Document",
  );
  $("result-count").textContent = `${matches.length}`;
  const list = $("results");
  list.replaceChildren();
  for (const n of matches.slice(0, 100)) {
    const b = button("", () => focusNode(n));
    b.className = "result";
    b.append(
      make("span", label(n), "name"),
      make(
        "small",
        `${n.kind} · ${sourceLabel(n)}`,
      ),
    );
    list.append(b);
  }
  if (matches.length > 100)
    list.append(
      make("p", "Showing the first 100 matches. Refine your search."),
    );
  if (!matches.length)
    list.append(make("p", "No matching documents or symbols."));
}
function focusNode(n) {
  mode = { type: n.kind === "Document" ? "document" : "symbol", id: n.id };
  renderGraph();
  showNode(n);
}
function renderGraph() {
  if (!model) return;
  const relations = model.relations.filter(accepted);
  let visible, links, title, total;
  if (mode.type === "documents") {
    const docs = model.nodes.filter((n) => n.kind === "Document");
    total = docs.length;
    visible = docs.slice(0, NODE_LIMIT);
    const docsByPath = new Map(visible.map((n) => [n.location.path, n.id]));
    const groups = new Map();
    for (const r of relations) {
      for (const sourcePath of sourcePaths(nodes.get(r.source))) {
        for (const targetPath of sourcePaths(nodes.get(r.target))) {
          const source = docsByPath.get(sourcePath),
            target = docsByPath.get(targetPath);
          if (!source || !target || source === target) continue;
          const key = JSON.stringify([source, target, r.kind]);
          if (!groups.has(key))
            groups.set(key, {
              id: `group-${groups.size}`,
              source,
              target,
              kind: r.kind,
              evidence: [],
            });
          groups.get(key).evidence.push(r);
        }
      }
    }
    links = [...groups.values()];
    title = "Documents";
  } else {
    const focus = nodes.get(mode.id);
    const seeds = new Set(
      mode.type === "document"
        ? model.nodes
            .filter((n) => n.location?.path === focus.location.path)
            .map((n) => n.id)
        : [mode.id],
    );
    const adjacent = new Set(seeds);
    for (const r of relations) {
      if (seeds.has(r.source) || seeds.has(r.target)) {
        adjacent.add(r.source);
        adjacent.add(r.target);
      }
    }
    total = adjacent.size;
    visible = [...adjacent]
      .slice(0, NODE_LIMIT)
      .map((id) => nodes.get(id))
      .filter(Boolean);
    const ids = new Set(visible.map((n) => n.id));
    links = relations
      .filter((r) => ids.has(r.source) && ids.has(r.target))
      .map((r) => ({ ...r, evidence: [r] }));
    title = label(focus);
  }
  const totalEdges = links.length;
  links = links.slice(0, EDGE_LIMIT);
  $("view-title").textContent = title;
  $("overview").classList.toggle("active", mode.type === "documents");
  $("scope").textContent =
    `${visible.length} / ${total} nodes · ${links.length} / ${totalEdges} displayed relationships${visible.length < total || links.length < totalEdges ? " · Display limit reached. Search or focus a smaller area." : ""}${mode.type === "documents" ? " · Edges aggregate cross-document evidence. Click to inspect." : " · One-hop neighborhood. Double-click a symbol to follow it."}`;
  $("graph-empty").hidden = visible.length !== 0;
  if (cy) cy.destroy();
  cy = cytoscape({
    container: $("graph"),
    elements: [
      ...visible.map((n) => ({
        data: {
          id: n.id,
          label: n.kind === "Document" ? n.location.path : n.name,
          kind: n.kind,
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
          "background-color": "#6486c4",
          label: "data(label)",
          color: "#334562",
          "font-size": 11,
          "min-zoomed-font-size": 7,
          "text-valign": "bottom",
          "text-margin-y": 8,
          "text-wrap": "ellipsis",
          "text-max-width": 150,
          width: 24,
          height: 24,
          "border-width": 2,
          "border-color": "#fff",
        },
      },
      {
        selector: 'node[kind = "Document"]',
        style: {
          shape: "roundrectangle",
          width: 38,
          height: 29,
          "background-color": "#335fae",
        },
      },
      {
        selector:
          'node[kind = "Class"], node[kind = "Struct"], node[kind = "Interface"]',
        style: {
          shape: "diamond",
          "background-color": "#309483",
          width: 32,
          height: 32,
        },
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
          "line-color": "#a2b7d6",
          "target-arrow-color": "#8ea6c9",
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
          "line-color": "#c39960",
          "target-arrow-color": "#c39960",
        },
      },
      { selector: ".dimmed", style: { opacity: 0.12 } },
      { selector: "edge:selected", style: { label: "data(label)" } },
      {
        selector: ":selected",
        style: {
          "background-color": "#d38639",
          "line-color": "#d38639",
          "target-arrow-color": "#d38639",
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
    if (mode.type === "documents") focusNode(n);
    else showNode(n);
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
function showComponents() {
  detailEpoch++;
  const panel = $("details");
  panel.replaceChildren(make("h2", "Repository components"));
  for (const component of model.components) {
    const item = make("div", undefined, "item");
    item.append(
      make("strong", component.name),
      make(
        "p",
        `${component.root} · ${component.language || "unknown language"}`,
        "meta",
      ),
    );
    for (const tool of component.packageTools || []) {
      item.append(
        make(
          "p",
          `${tool.name}${tool.version ? "@" + tool.version : ""} · ${tool.evidence.join(", ")}`,
          "meta",
        ),
      );
    }
    panel.append(item);
  }
  panel.append(
    make(
      "p",
      "Package tools are identified from captured manifests and lockfiles. They are not executed.",
    ),
  );
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
  panel.append(button("Explore this neighborhood", () => focusNode(n)));
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
      if (n) item.append(button(label(n), () => focusNode(n)));
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
function showDiagnostics(path) {
  ++detailEpoch;
  const panel = $("details");
  panel.replaceChildren(
    make("h2", path ? "Document diagnostics" : "Analysis diagnostics"),
  );
  const issues = [
    ...(path ? [] : model.snapshot.diagnostics),
    ...model.diagnostics.filter((d) => !path || d.location.path === path),
  ];
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
        "Showing the first 200 diagnostics. Select a document to narrow the list.",
      ),
    );
}
$("search").addEventListener("input", renderResults);
$("confidence").addEventListener("change", renderGraph);
$("overview").addEventListener("click", () => {
  mode = { type: "documents" };
  renderGraph();
});
$("fit").addEventListener("click", () => cy?.fit(undefined, 45));
$("components").addEventListener("click", showComponents);
$("diagnostics").addEventListener("click", () => showDiagnostics());
$("refresh").addEventListener("click", async () => {
  $("refresh").disabled = true;
  $("refresh").textContent = "Building…";
  notice("Capturing a new snapshot. The current graph remains available.");
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
api("/api/graph")
  .then(setModel)
  .catch((err) => notice(`Cannot load graph: ${err.message}`));
