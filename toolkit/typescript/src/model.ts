import type { Component, Product, Repository } from "@compforge/harness-common";

/** Select a checkout's working tree (default), index, or resolved commit.
 * A single timeout covers Git and metadata reads. Cancellation belongs to the caller.
 */
export interface InspectOptions {
  readonly repository?: string;
  readonly head?: string;
  readonly staged?: boolean;
  readonly signal?: AbortSignal;
  readonly timeoutMs?: number;
}

export interface PackageTool {
  readonly name: string;
  readonly version?: string;
  readonly evidence: readonly string[];
}

/** Identity comes from common; layout and static tool observations belong to repocli. */
export interface ComponentBinding extends Component {
  readonly root: string;
  readonly products: readonly Product[];
  readonly manifests?: readonly string[];
  readonly packageTools?: readonly PackageTool[];
}

export interface Layout {
  readonly repository: Repository | null;
  readonly components: readonly ComponentBinding[];
}

export interface Diagnostic {
  readonly code: string;
  readonly message: string;
}

/** Completeness covers catalog/metadata observation, not buildability or source contents. */
export interface InspectReport extends Layout {
  readonly schemaVersion: 1;
  readonly checkout: string;
  readonly input: "working_tree" | "index" | "commit";
  readonly head?: string;
  readonly complete: boolean;
  readonly diagnostics: readonly Diagnostic[];
}

/** Find the deepest owning component, including for a deleted repository-relative path. */
export function owner(layout: Layout, path: string): ComponentBinding | undefined {
  let found: ComponentBinding | undefined;
  for (const component of layout.components) {
    const root = component.root;
    if (root === "." || path === root || path.startsWith(root + "/")) {
      if (!found || found.root === "." || root.length > found.root.length) found = component;
    }
  }
  return found;
}
