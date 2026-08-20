import type { Scan, ScanSource } from "./types";

export class FakeSource implements ScanSource {
  readonly id: string;
  readonly label: string;
  available: boolean;
  shouldThrowOnStart = false;
  shouldThrowOnStop = false;

  started = false;
  stopped = false;
  startCount = 0;
  stopCount = 0;

  private emitCallback: ((rawOrScan: string | Scan) => void) | null = null;

  constructor(id = "fake-scanner", label = "Fake Scanner", available = true) {
    this.id = id;
    this.label = label;
    this.available = available;
  }

  async isAvailable(): Promise<boolean> {
    return this.available;
  }

  async start(emit: (rawOrScan: string | Scan) => void): Promise<void> {
    if (this.shouldThrowOnStart) {
      throw new Error(`FakeSource[${this.id}] failed to start`);
    }
    this.started = true;
    this.stopped = false;
    this.startCount++;
    this.emitCallback = emit;
  }

  async stop(): Promise<void> {
    if (this.shouldThrowOnStop) {
      throw new Error(`FakeSource[${this.id}] failed to stop`);
    }
    this.started = false;
    this.stopped = true;
    this.stopCount++;
    this.emitCallback = null;
  }

  simulateScan(rawOrScan: string | Scan): void {
    if (!this.started || !this.emitCallback) {
      throw new Error(
        `Cannot simulate scan: FakeSource[${this.id}] is not currently started`
      );
    }
    this.emitCallback(rawOrScan);
  }

  simulateError(err: Error): void {
    if (!this.started || !this.emitCallback) {
      throw new Error(
        `Cannot simulate error: FakeSource[${this.id}] is not currently started`
      );
    }
    // Simulate a crashing emitter
    const cb = this.emitCallback;
    cb(
      // Pass a proxy or trigger an unhandled throw in callback evaluation
      {
        get token(): string {
          throw err;
        },
        source: this.id,
        scannedAt: new Date().toISOString(),
      } as unknown as Scan
    );
  }
}
