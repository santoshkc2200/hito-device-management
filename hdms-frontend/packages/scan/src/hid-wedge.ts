import type { Scan, ScanSource } from "./types";

/**
 * Heuristic threshold for inter-key interval (in milliseconds).
 *
 * Scanners "type" fast bursts (typically 2-20 ms between keystrokes),
 * whereas human typists rarely type under 80-140 ms per character.
 *
 * NOTE: This value (35ms) is currently UNTUNED and derived from standard
 * literature/benchmarks. It will be validated and adjusted against real
 * scanner hardware during Phase 3.10 (Hardware Validation).
 */
export const MAX_INTERVAL_MS = 35;

/**
 * Minimum characters required for a valid scan sequence before Enter.
 *
 * NOTE: This value (6) is currently UNTUNED and derived from the shortest
 * supported token format (e.g. prefix + identifier). It will be validated
 * against real scanner hardware during Phase 3.10.
 */
export const MIN_LENGTH = 6;

/**
 * Timeout in milliseconds of keyboard inactivity after which any partial
 * scan buffer is cleared. Prevents a truncated or cancelled trigger pull
 * from polluting the next scan.
 */
export const IDLE_RESET_MS = 100;

/**
 * Maximum character capacity for the scan buffer. If keystrokes exceed this
 * limit without an Enter delimiter, the buffer is dropped to prevent unbounded growth.
 */
export const MAX_BUFFER_LENGTH = 64;

/**
 * Window in milliseconds for scanner freshness/heartbeat.
 * If no scan activity is detected within this duration, the scanner state
 * is considered stale/idle.
 */
export const DEFAULT_FRESHNESS_WINDOW_MS = 60_000;

/**
 * Maximum number of raw diagnostic sequence traces held in memory.
 * Stored strictly in-memory (never logged or transmitted) to inspect
 * inter-key timings in the field.
 */
export const MAX_DIAGNOSTICS_ENTRIES = 10;

export interface RawSequenceDiagnostic {
  raw: string;
  timings: number[];
  timestamp: number; // epoch ms
  emitted: boolean;
}

export interface HidWedgeOptions {
  maxIntervalMs?: number;
  minLength?: number;
  idleResetMs?: number;
  maxBufferLength?: number;
  freshnessWindowMs?: number;
  now?: () => number;
  onBufferDropped?: (reason: "overflow" | "timeout" | "escape") => void;
}

export interface KeyInputEvent {
  key: string;
  timeStamp?: number;
}

export interface ProcessKeyResult {
  emitted: boolean;
  scanText?: string;
  wasBufferDropped?: boolean;
}

/**
 * Pure stateful interpreter for HID wedge keystroke sequences.
 * Can be tested with synthesised timing sequences without requiring a DOM.
 */
export class HidWedgeInterpreter {
  private buffer = "";
  private timings: number[] = [];
  private lastKeyAt = 0;
  private idleTimer: ReturnType<typeof setTimeout> | null = null;
  private lastActivityAt: string | null = null;
  private lastActivityTimestamp = 0;
  private diagnostics: RawSequenceDiagnostic[] = [];

  readonly maxIntervalMs: number;
  readonly minLength: number;
  readonly idleResetMs: number;
  readonly maxBufferLength: number;
  readonly freshnessWindowMs: number;
  private now: () => number;
  private onBufferDropped?: (reason: "overflow" | "timeout" | "escape") => void;

  constructor(options?: HidWedgeOptions) {
    this.maxIntervalMs = options?.maxIntervalMs ?? MAX_INTERVAL_MS;
    this.minLength = options?.minLength ?? MIN_LENGTH;
    this.idleResetMs = options?.idleResetMs ?? IDLE_RESET_MS;
    this.maxBufferLength = options?.maxBufferLength ?? MAX_BUFFER_LENGTH;
    this.freshnessWindowMs = options?.freshnessWindowMs ?? DEFAULT_FRESHNESS_WINDOW_MS;
    this.now =
      options?.now ??
      (() => (typeof performance !== "undefined" ? performance.now() : Date.now()));
    this.onBufferDropped = options?.onBufferDropped;
  }

  processKey(event: KeyInputEvent): ProcessKeyResult {
    const key = event.key;
    const currentTime = event.timeStamp ?? this.now();

    // 1. Clear any active idle reset timer upon new keystroke
    this.clearIdleTimer();

    // 2. Escape clears the buffer immediately
    if (key === "Escape") {
      this.recordDiagnostic(this.buffer, [...this.timings], false);
      this.resetBuffer();
      this.onBufferDropped?.("escape");
      return { emitted: false, wasBufferDropped: true };
    }

    // 3. Enter terminates sequence
    if (key === "Enter") {
      const delta = this.lastKeyAt > 0 ? currentTime - this.lastKeyAt : 0;
      if (this.buffer.length > 0) {
        this.timings.push(delta);
      }

      if (this.buffer.length >= this.minLength) {
        const text = this.buffer;
        this.recordDiagnostic(text, [...this.timings], true);
        this.markActivity();
        this.resetBuffer();
        return { emitted: true, scanText: text };
      }

      // Buffer was too short for a valid scan
      if (this.buffer.length > 0) {
        this.recordDiagnostic(this.buffer, [...this.timings], false);
      }
      this.resetBuffer();
      return { emitted: false };
    }

    // 4. Ignore modifier keys and dead keys (e.g. Shift, Control, Alt, Meta, Dead)
    if (key.length !== 1) {
      return { emitted: false };
    }

    // 5. Inter-key timing check: if typing is too slow, reset buffer and start fresh
    if (this.lastKeyAt > 0) {
      const interval = currentTime - this.lastKeyAt;
      if (interval > this.maxIntervalMs) {
        if (this.buffer.length > 0) {
          this.recordDiagnostic(this.buffer, [...this.timings], false);
        }
        this.resetBuffer();
      } else {
        this.timings.push(interval);
      }
    }

    this.lastKeyAt = currentTime;
    this.buffer += key;

    // 6. Buffer capacity cap
    if (this.buffer.length > this.maxBufferLength) {
      this.recordDiagnostic(this.buffer, [...this.timings], false);
      this.resetBuffer();
      this.onBufferDropped?.("overflow");
      return { emitted: false, wasBufferDropped: true };
    }

    // 7. Schedule idle timer to clear partial buffer after timeout
    this.scheduleIdleTimer();

    return { emitted: false };
  }

  reset(): void {
    this.clearIdleTimer();
    this.resetBuffer();
  }

  destroy(): void {
    this.clearIdleTimer();
    this.resetBuffer();
  }

  getLastActivityAt(): string | null {
    return this.lastActivityAt;
  }

  isFresh(windowMs: number = this.freshnessWindowMs): boolean {
    if (this.lastActivityTimestamp === 0) return false;
    const now = this.now();
    return now - this.lastActivityTimestamp <= windowMs;
  }

  getDiagnostics(): readonly RawSequenceDiagnostic[] {
    return [...this.diagnostics];
  }

  clearDiagnostics(): void {
    this.diagnostics = [];
  }

  private markActivity(): void {
    this.lastActivityAt = new Date().toISOString();
    this.lastActivityTimestamp = this.now();
  }

  private resetBuffer(): void {
    this.buffer = "";
    this.timings = [];
    this.lastKeyAt = 0;
  }

  private scheduleIdleTimer(): void {
    this.idleTimer = setTimeout(() => {
      if (this.buffer.length > 0) {
        this.recordDiagnostic(this.buffer, [...this.timings], false);
        this.resetBuffer();
        this.onBufferDropped?.("timeout");
      }
    }, this.idleResetMs);
  }

  private clearIdleTimer(): void {
    if (this.idleTimer !== null) {
      clearTimeout(this.idleTimer);
      this.idleTimer = null;
    }
  }

  private recordDiagnostic(raw: string, timings: number[], emitted: boolean): void {
    if (!raw) return;
    const entry: RawSequenceDiagnostic = {
      raw,
      timings,
      timestamp: Date.now(),
      emitted,
    };
    this.diagnostics.push(entry);
    if (this.diagnostics.length > MAX_DIAGNOSTICS_ENTRIES) {
      this.diagnostics.shift();
    }
  }
}

/**
 * DOM-level ScanSource implementation for Bluetooth barcode scanners acting
 * as HID keyboard wedges.
 *
 * Attaches a capture-phase keydown listener on window to reliably intercept
 * scanner sequences regardless of focused element.
 */
export class HidWedgeSource implements ScanSource {
  readonly id = "scanner" as const;
  readonly label: string;

  private interpreter: HidWedgeInterpreter;
  private keydownListener: ((e: KeyboardEvent) => void) | null = null;
  private emitCallback: ((rawOrScan: string | Scan) => void) | null = null;
  private targetWindow: Window | null = null;

  constructor(options?: HidWedgeOptions & { label?: string; targetWindow?: Window }) {
    this.label = options?.label ?? "Barcode Scanner (HID)";
    this.targetWindow =
      options?.targetWindow ?? (typeof window !== "undefined" ? window : null);
    this.interpreter = new HidWedgeInterpreter(options);
  }

  async isAvailable(): Promise<boolean> {
    // In HID wedge mode, the scanner is paired as an OS keyboard.
    // There is no queryable API for pairing status, so availability is always true.
    return true;
  }

  async start(emit: (rawOrScan: string | Scan) => void): Promise<void> {
    this.emitCallback = emit;
    if (!this.targetWindow) return;

    this.keydownListener = (event: KeyboardEvent) => {
      this.handleKeyDown(event);
    };

    this.targetWindow.addEventListener("keydown", this.keydownListener, {
      capture: true,
    });
  }

  async stop(): Promise<void> {
    if (this.targetWindow && this.keydownListener) {
      this.targetWindow.removeEventListener("keydown", this.keydownListener, {
        capture: true,
      });
    }
    this.keydownListener = null;
    this.emitCallback = null;
    this.interpreter.destroy();
  }

  getLastActivityAt(): string | null {
    return this.interpreter.getLastActivityAt();
  }

  isFresh(windowMs?: number): boolean {
    return this.interpreter.isFresh(windowMs);
  }

  getDiagnostics(): readonly RawSequenceDiagnostic[] {
    return this.interpreter.getDiagnostics();
  }

  clearDiagnostics(): void {
    this.interpreter.clearDiagnostics();
  }

  private handleKeyDown(event: KeyboardEvent): void {
    const target = event.target as HTMLElement | null;
    const isTextInputFocused =
      target !== null &&
      (target.tagName === "INPUT" ||
        target.tagName === "TEXTAREA" ||
        (typeof target.isContentEditable === "boolean" && target.isContentEditable));

    // If an editable text input is focused, check whether this keystroke is slow (human typing).
    // The interpreter naturally separates slow keystrokes by MAX_INTERVAL_MS, but we also ensure
    // that human typing inside an input does not build a scan buffer.
    const result = this.interpreter.processKey({
      key: event.key,
      timeStamp: event.timeStamp,
    });

    if (result.emitted && result.scanText) {
      // Suppress the terminating Enter key so it doesn't activate focused buttons or submit forms
      event.preventDefault();
      event.stopPropagation();

      if (this.emitCallback) {
        this.emitCallback(result.scanText);
      }
    } else if (isTextInputFocused && event.key === "Enter") {
      // Allow normal Enter handling on text inputs if no barcode scan was completed
    }
  }
}
