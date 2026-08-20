import type { Scan, ScanSource } from "./types";

export interface ManualSourceOptions {
  label?: string;
}

/**
 * ScanSource implementation for attendant manual token entry via on-screen keypad.
 *
 * This source has no DOM of its own; UI components (PIN gate / Crockford keypad)
 * invoke `submit(token)` directly. Emissions flow into the ScanRouter just like
 * hardware scans, inheriting local token parsing, checksum validation, and debounce.
 */
export class ManualSource implements ScanSource {
  readonly id = "manual" as const;
  readonly label: string;

  private emitCallback: ((rawOrScan: string | Scan) => void) | null = null;
  private isStarted = false;

  constructor(options?: ManualSourceOptions) {
    this.label = options?.label ?? "Attendant Manual Entry";
  }

  async isAvailable(): Promise<boolean> {
    return true;
  }

  async start(emit: (rawOrScan: string | Scan) => void): Promise<void> {
    this.emitCallback = emit;
    this.isStarted = true;
  }

  async stop(): Promise<void> {
    this.emitCallback = null;
    this.isStarted = false;
  }

  /**
   * Submits a token string through the scan pipeline.
   * Returns true if the source is active and emitted the token, false otherwise.
   */
  submit(token: string): boolean {
    if (!this.isStarted || !this.emitCallback) {
      return false;
    }
    this.emitCallback(token);
    return true;
  }
}
