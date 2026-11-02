import { runGit } from "./operations.js";

export interface Remote {
  readonly host: string;
  readonly path: string;
}

/** Network Git identity, without provider selection or credentials. */
export function parseRemoteUrl(value: string): Remote | undefined {
  let host: string, path: string;
  if (value.includes("://")) {
    try {
      const url = new URL(value);
      // Identity parsing does not decide which transports Git may execute.
      if (url.protocol === "file:") return undefined;
      host = url.hostname;
      path = decodeURIComponent(url.pathname).replace(/^\//, "");
    } catch { return undefined; }
  } else {
    const colon = value.indexOf(":");
    if (colon < 0) return undefined;
    host = value.slice(0, colon).replace(/^[^@]*@/, "");
    if (host.includes("/") || host.includes("\\")) return undefined;
    path = value.slice(colon + 1);
  }
  path = path.replace(/\/$/, "").replace(/\.git$/, "");
  return host && path ? { host: host.toLowerCase(), path } : undefined;
}

export function remote(repo: string, name = "origin"): Remote | undefined {
  const result = runGit(repo, ["remote", "get-url", "--", name]);
  return result.ok ? parseRemoteUrl(result.stdout) : undefined;
}
