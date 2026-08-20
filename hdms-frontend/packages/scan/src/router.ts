import {
  parseToken,
  formatToken,
  TokenParseError,
} from "@hdms/domain";
import type { Scan, ScanSource, ScanEvent, Unsubscribe } from "./types";

export interface ScanRouterOptions {
  now?: () => number;
  debounceMs?: number;
}

const DEFAULT_DEBOUNCE_MS = 1500;

export class ScanRouter {
  private sources: Map<string, ScanSource> = new Map();
  private activeSources: Set<string> = new Set();
  private listeners: Set<(event: ScanEvent) => void> = new Set();
  private recentScans: Map<string, number> = new Map();
  private now: () => number;
  private debounceMs: number;
  private started = false;

  constructor(options?: ScanRouterOptions) {
    this.now =
      options?.now ??
      (() =>
        typeof performance !== "undefined"
          ? performance.now()
          : Date.now());
    this.debounceMs = options?.debounceMs ?? DEFAULT_DEBOUNCE_MS;
  }

  register(source: ScanSource): void {
    if (this.sources.has(source.id)) {
      this.unregister(source.id);
    }
    this.sources.set(source.id, source);

    if (this.started) {
      void this.startSource(source);
    }
  }

  unregister(id: string): void {
    const source = this.sources.get(id);
    if (!source) return;

    if (this.activeSources.has(id)) {
      this.activeSources.delete(id);
      try {
        void source.stop();
      } catch {
        // Ignore errors on unregister stop
      }
    }
    this.sources.delete(id);
  }

  getRegisteredSources(): readonly ScanSource[] {
    return Array.from(this.sources.values());
  }

  async start(): Promise<void> {
    this.started = true;
    const promises = Array.from(this.sources.values()).map((source) =>
      this.startSource(source)
    );
    await Promise.all(promises);
  }

  async stop(): Promise<void> {
    this.started = false;
    const stopPromises: Promise<void>[] = [];

    for (const source of this.sources.values()) {
      try {
        stopPromises.push(
          Promise.resolve(source.stop()).catch(() => {
            // Ignore errors during mass teardown
          })
        );
      } catch {
        // Ignore synchronous throws
      }
    }

    this.activeSources.clear();
    this.recentScans.clear();
    await Promise.all(stopPromises);
  }

  subscribe(listener: (event: ScanEvent) => void): Unsubscribe {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  }

  private async startSource(source: ScanSource): Promise<void> {
    try {
      const available = await source.isAvailable();
      if (!available) {
        this.emitEvent({
          kind: "source-state",
          id: source.id,
          available: false,
        });
        return;
      }

      await source.start((rawOrScan) => {
        this.handleSourceEmission(source, rawOrScan);
      });

      this.activeSources.add(source.id);
      this.emitEvent({
        kind: "source-state",
        id: source.id,
        available: true,
      });
    } catch {
      this.activeSources.delete(source.id);
      this.emitEvent({
        kind: "source-state",
        id: source.id,
        available: false,
      });
    }
  }

  private handleSourceEmission(
    source: ScanSource,
    rawOrScan: string | Scan
  ): void {
    try {
      const raw =
        typeof rawOrScan === "string" ? rawOrScan : rawOrScan.raw ?? rawOrScan.token;

      // 1. Local parse & token validation
      let canonicalToken: string;
      try {
        const parsed = parseToken(raw);
        canonicalToken = formatToken(parsed);
      } catch (err) {
        if (err instanceof TokenParseError) {
          this.emitEvent({
            kind: "invalid",
            raw,
            reason: err.reason,
          });
          return;
        }
        this.emitEvent({
          kind: "invalid",
          raw,
          reason: "invalid-format",
        });
        return;
      }

      // 2. Client-side debounce check on normalized token
      const currentTime = this.now();
      this.pruneRecentScans(currentTime);

      const lastSeen = this.recentScans.get(canonicalToken);
      if (lastSeen !== undefined && currentTime - lastSeen < this.debounceMs) {
        this.emitEvent({
          kind: "duplicate",
          token: canonicalToken,
        });
        return;
      }

      // Record in debounce cache
      this.recentScans.set(canonicalToken, currentTime);

      // 3. Emit valid scan
      const scan: Scan = {
        token: canonicalToken,
        source: source.id,
        scannedAt: new Date().toISOString(),
        raw,
      };

      this.emitEvent({
        kind: "scan",
        scan,
      });
    } catch {
      // Isolate any unforeseen error inside the source handler
      this.activeSources.delete(source.id);
      this.emitEvent({
        kind: "source-state",
        id: source.id,
        available: false,
      });
    }
  }

  private pruneRecentScans(currentTime: number): void {
    for (const [token, timestamp] of this.recentScans.entries()) {
      if (currentTime - timestamp >= this.debounceMs) {
        this.recentScans.delete(token);
      }
    }
  }

  private emitEvent(event: ScanEvent): void {
    for (const listener of this.listeners) {
      try {
        listener(event);
      } catch {
        // Isolate subscriber errors from affecting the router or other listeners
      }
    }
  }
}
