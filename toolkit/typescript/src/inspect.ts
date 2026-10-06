import { realpath } from "node:fs/promises";
import { isDeepStrictEqual } from "node:util";
import { Git } from "./git.js";
import { load } from "./layout.js";
import type { InspectOptions, InspectReport } from "./model.js";

/** Inspect organization in-process, reading Git paths and layout metadata only.
 * Throws for invalid inputs, read failures, limits or cancellation. A concurrently
 * changed catalog returns an incomplete report; consumers decide how to use it.
 */
export async function inspect(options: InspectOptions = {}): Promise<InspectReport> {
  if (options.head && options.staged) throw new Error("head and staged are mutually exclusive");
  const timeout = options.timeoutMs ?? 5_000;
  if (!Number.isSafeInteger(timeout) || timeout <= 0 || timeout > 2_147_483_647) throw new Error("timeoutMs must be a positive 32-bit integer");
  const deadline = new AbortController();
  const timer = setTimeout(() => deadline.abort(new Error("inspection timed out")), timeout);
  timer.unref();
  const signal = options.signal ? AbortSignal.any([options.signal, deadline.signal]) : deadline.signal;
  try {
    signal.throwIfAborted();
    const initial = new Git(options.repository || process.cwd(), signal);
    const root = await realpath((await initial.run(["rev-parse", "--show-toplevel"])).toString().trimEnd());
    const git = new Git(root, signal);
    const head = options.head ? (await git.run(["rev-parse", "--verify", "--end-of-options", options.head + "^{commit}"])).toString().trim() : "";
    const files = await git.catalog(head, options.staged ?? false);
    const origin = (await git.run(["config", "--get", "remote.origin.url"], undefined, true)).toString().trimEnd();
    const layout = load(files, origin);
    const complete = head !== "" || isDeepStrictEqual(files, await git.catalog(head, options.staged ?? false));
    signal.throwIfAborted();
    return { ...layout, schemaVersion: 1, checkout: root,
      input: head ? "commit" : options.staged ? "index" : "working_tree", ...(head ? { head } : {}), complete,
      diagnostics: complete ? [] : [{ code: "inspection_changed", message: "repository paths or layout metadata changed during inspection" }],
    };
  } finally { clearTimeout(timer); }
}
