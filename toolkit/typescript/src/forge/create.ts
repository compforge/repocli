import { GitHubForgeReader } from "./github.js";
import { GitLabForgeReader } from "./gitlab.js";
import type { ForgeReader, ForgeReaderConfig, ForgeReaderOptions } from "./model.js";

export function createForgeReader(config: ForgeReaderConfig, options: ForgeReaderOptions = {}): ForgeReader {
  return config.provider === "github" ? new GitHubForgeReader(config, options) : new GitLabForgeReader(config, options);
}
