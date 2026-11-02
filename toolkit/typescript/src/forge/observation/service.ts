import type { PullRequestState } from "@compforge/harness-common";
import { setTimeout as delay } from "node:timers/promises";
import { ForgeError, type ForgeReadIssue, type PullRequestKey, type PullRequestScope } from "../model.js";
import { ObservationEngine } from "./engine.js";
import type { ObservationReader, PullRequestObservationOptions } from "./model.js";

export interface PullRequestStatus {
  readonly key: PullRequestKey;
  readonly state: PullRequestState;
  readonly observedAt: string;
}
export interface PullRequestObserverOptions extends PullRequestObservationOptions {
  /** Optional discovery interests; directly queried PRs are maintained automatically. */
  readonly scopes?: readonly PullRequestScope[];
  readonly initialSince?: string;
  /** Disable the internal loop when the host drives bounded maintain calls. */
  readonly background?: boolean;
}
export interface PullRequestObserverHealth {
  readonly running: boolean;
  readonly checkedAt?: string;
  readonly issues: readonly ForgeReadIssue[];
  readonly error?: string;
}
export class PullRequestStatusUnavailable extends ForgeError {
  constructor(readonly key: PullRequestKey, readonly issue?: ForgeReadIssue) {
    super(`PR lifecycle unavailable for ${key.repository.path}#${key.number}: ${issue?.message ?? "no current observation"}`);
  }
}

/**
 * @spec Business callers query lifecycle status; cache selection, durable refresh and scheduling remain internal.
 * @spec Only a current or confirmed-terminal lifecycle is returned. A stale nonterminal observation is not a successful query.
 * @spec The host owns this resource's lifetime through close; closing aborts background and cooperative foreground requests.
 * @see {@link component://docs/pr.md#持续观察}
 * @case tests/forge-observation.test.mjs
 */
export class PullRequestObserver {
  readonly #engine: ObservationEngine;
  readonly #options: PullRequestObserverOptions;
  readonly #controller = new AbortController();
  readonly #background: Promise<void>;
  #health: PullRequestObserverHealth = { running: true, issues: [] };
  constructor(reader: ObservationReader, options: PullRequestObserverOptions) {
    this.#options = options;
    this.#engine = new ObservationEngine(reader, options);
    const interval = Math.min(options.openIntervalMs ?? 60_000, options.closedIntervalMs ?? 600_000, options.retryIntervalMs ?? 30_000);
    this.#background = options.background === false ? Promise.resolve() : this.#maintain(options, interval);
    if (options.background === false) this.#health = { running: false, issues: [] };
  }
  get health(): PullRequestObserverHealth { return structuredClone(this.#health); }
  async getPullRequest(key: PullRequestKey, options: { signal?: AbortSignal } = {}): Promise<PullRequestStatus> {
    const signal = options.signal ? AbortSignal.any([options.signal, this.#controller.signal]) : this.#controller.signal;
    signal.throwIfAborted();
    const result = await this.#engine.get(key, { signal });
    if (!result.fresh || !result.snapshot?.state || !result.observedAt) throw new PullRequestStatusUnavailable(key, result.lastError);
    return { key: result.key, state: result.snapshot.state, observedAt: result.observedAt };
  }
  async maintain(): Promise<PullRequestObserverHealth> {
    const result = await this.#engine.poll({ signal: this.#controller.signal });
    const discovery = this.#options.scopes?.length ? await this.#engine.poll({
      scopes: this.#options.scopes, keys: [], refresh: false, initialSince: this.#options.initialSince,
      signal: this.#controller.signal,
    }) : undefined;
    this.#health = { ...this.#health, checkedAt: new Date().toISOString(), issues: [...result.issues, ...(discovery?.issues ?? [])] };
    return this.health;
  }
  async close(): Promise<void> {
    this.#controller.abort();
    await this.#background;
  }
  async #maintain(options: PullRequestObserverOptions, interval: number): Promise<void> {
    const signal = this.#controller.signal;
    try {
      while (!signal.aborted) {
        let pending = false;
        try {
          const refresh = await this.#engine.poll({ signal });
          pending = refresh.pending;
          const issues = [...refresh.issues];
          if (options.scopes?.length) {
            const discovery = await this.#engine.poll({ scopes: options.scopes, keys: [], refresh: false, initialSince: options.initialSince, signal });
            pending ||= discovery.pending;
            issues.push(...discovery.issues);
          }
          this.#health = { running: true, checkedAt: new Date().toISOString(), issues };
        } catch (error) {
          signal.throwIfAborted();
          this.#health = { ...this.#health, error: error instanceof Error ? error.message : String(error) };
        }
        await delay(pending ? Math.min(interval, 1000) : interval, undefined, { signal });
      }
    } catch (error) { if (!signal.aborted) throw error; }
    finally { this.#health = { ...this.#health, running: false }; }
  }
}
