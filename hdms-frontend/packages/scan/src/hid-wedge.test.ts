import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import {
  HidWedgeInterpreter,
  HidWedgeSource,
  MAX_INTERVAL_MS,
  MIN_LENGTH,
  IDLE_RESET_MS,
  MAX_BUFFER_LENGTH,
  DEFAULT_FRESHNESS_WINDOW_MS,
  MAX_DIAGNOSTICS_ENTRIES,
} from "./hid-wedge";
import { ScanRouter } from "./router";
import type { ScanEvent } from "./types";

describe("HidWedgeInterpreter", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("exports untuned timing and buffer constants with literature defaults", () => {
    expect(MAX_INTERVAL_MS).toBe(35);
    expect(MIN_LENGTH).toBe(6);
    expect(IDLE_RESET_MS).toBe(100);
    expect(MAX_BUFFER_LENGTH).toBe(64);
    expect(DEFAULT_FRESHNESS_WINDOW_MS).toBe(60_000);
    expect(MAX_DIAGNOSTICS_ENTRIES).toBe(10);
  });

  function replayKeys(
    interpreter: HidWedgeInterpreter,
    keys: string[],
    options: { gapMs: number; initialTime?: number }
  ): string[] {
    const emittedScans: string[] = [];
    let currentTime = options.initialTime ?? 1000;

    for (const key of keys) {
      vi.advanceTimersByTime(options.gapMs);
      currentTime += options.gapMs;
      const result = interpreter.processKey({
        key,
        timeStamp: currentTime,
      });
      if (result.emitted && result.scanText) {
        emittedScans.push(result.scanText);
      }
    }

    return emittedScans;
  }

  it("treats fast keystrokes ending in Enter as one scan (8 ms gaps)", () => {
    const interpreter = new HidWedgeInterpreter();
    const token = "HD-U-7K3M9QXA2F-4";
    const keys = [...token.split(""), "Enter"];

    const scans = replayKeys(interpreter, keys, { gapMs: 8 });

    expect(scans).toEqual(["HD-U-7K3M9QXA2F-4"]);
  });

  it("ignores human typing at the same keys (140 ms gaps)", () => {
    const interpreter = new HidWedgeInterpreter();
    const token = "HD-U-7K3M9QXA2F-4";
    const keys = [...token.split(""), "Enter"];

    const scans = replayKeys(interpreter, keys, { gapMs: 140 });

    expect(scans).toEqual([]);
  });

  it("boundaryAtThreshold — 34 ms is a scan, 36 ms is not", () => {
    const token = "HD-U-7K3M9QXA2F-4";
    const keys = [...token.split(""), "Enter"];

    // 34 ms <= MAX_INTERVAL_MS (35 ms) -> Valid scan
    const fastInterpreter = new HidWedgeInterpreter();
    const fastScans = replayKeys(fastInterpreter, keys, { gapMs: 34 });
    expect(fastScans).toEqual([token]);

    // 36 ms > MAX_INTERVAL_MS (35 ms) -> Treated as human typing / separate keystrokes
    const slowInterpreter = new HidWedgeInterpreter();
    const slowScans = replayKeys(slowInterpreter, keys, { gapMs: 36 });
    expect(slowScans).toEqual([]);
  });

  it("recovers from partial read followed by real scan", () => {
    const interpreter = new HidWedgeInterpreter();

    // Partial read (e.g. trigger released mid-barcode, 3 chars typed fast, no Enter)
    replayKeys(interpreter, ["H", "D", "-"], { gapMs: 10 });

    // Wait for idle reset (100ms)
    vi.advanceTimersByTime(IDLE_RESET_MS + 20);

    // Subsequent full scan arrives
    const fullToken = "HD-U-7K3M9QXA2F-4";
    const fullKeys = [...fullToken.split(""), "Enter"];
    const scans = replayKeys(interpreter, fullKeys, { gapMs: 10 });

    expect(scans).toEqual([fullToken]);
  });

  it("idleResetClearsAStalePartialBuffer", () => {
    const dropped: string[] = [];
    const interpreter = new HidWedgeInterpreter({
      onBufferDropped: (reason) => dropped.push(reason),
    });

    // Send 3 fast keys without Enter
    replayKeys(interpreter, ["H", "D", "-"], { gapMs: 8 });
    expect(dropped).toEqual([]);

    // Advance past idle reset timer
    vi.advanceTimersByTime(IDLE_RESET_MS + 10);
    expect(dropped).toEqual(["timeout"]);

    // If an Enter comes now, it is not emitted because buffer was cleared
    const result = interpreter.processKey({ key: "Enter", timeStamp: 2000 });
    expect(result.emitted).toBe(false);
  });

  it("rejects sequences shorter than minLength", () => {
    const interpreter = new HidWedgeInterpreter({ minLength: MIN_LENGTH });

    // Short sequence (< 6 chars) ending in Enter
    const shortKeys = ["A", "B", "C", "Enter"];
    const scans = replayKeys(interpreter, shortKeys, { gapMs: 8 });

    expect(scans).toEqual([]);
  });

  it("suppresses terminating Enter only on emit", () => {
    const interpreter = new HidWedgeInterpreter();

    // Valid scan ending with Enter
    const validKeys = ["H", "D", "-", "U", "-", "1", "2", "3"];
    for (const key of validKeys) {
      interpreter.processKey({ key, timeStamp: 1000 });
    }
    const emitResult = interpreter.processKey({ key: "Enter", timeStamp: 1010 });
    expect(emitResult.emitted).toBe(true);
    expect(emitResult.scanText).toBe("HD-U-123");

    // Lone Enter or non-scan Enter
    const loneEnterResult = interpreter.processKey({ key: "Enter", timeStamp: 2000 });
    expect(loneEnterResult.emitted).toBe(false);
  });

  it("ignores modifier and dead keys", () => {
    const interpreter = new HidWedgeInterpreter();

    // Mixed with Shift, Control, Alt, Dead
    const keys = [
      "Shift",
      "H",
      "D",
      "Control",
      "-",
      "U",
      "-",
      "Alt",
      "7",
      "K",
      "3",
      "Dead",
      "M",
      "9",
      "Enter",
    ];

    const scans = replayKeys(interpreter, keys, { gapMs: 8 });
    expect(scans).toEqual(["HD-U-7K3M9"]);
  });

  it("Escape clears buffer immediately", () => {
    const dropped: string[] = [];
    const interpreter = new HidWedgeInterpreter({
      onBufferDropped: (reason) => dropped.push(reason),
    });

    replayKeys(interpreter, ["H", "D", "-"], { gapMs: 8 });
    const escapeResult = interpreter.processKey({ key: "Escape", timeStamp: 1100 });
    expect(escapeResult.wasBufferDropped).toBe(true);
    expect(dropped).toEqual(["escape"]);

    // Next Enter should not emit anything
    const enterResult = interpreter.processKey({ key: "Enter", timeStamp: 1110 });
    expect(enterResult.emitted).toBe(false);
  });

  it("interleavedScanAndHumanTypingDoNotMerge", () => {
    const interpreter = new HidWedgeInterpreter();

    // Attendant types slowly: "A", then pauses 150ms
    replayKeys(interpreter, ["A"], { gapMs: 150, initialTime: 1000 });
    vi.advanceTimersByTime(IDLE_RESET_MS + 20);

    // Fast scanner sequence arrives
    const token = "HD-U-7K3M9QXA2F-4";
    const scanKeys = [...token.split(""), "Enter"];
    const scans = replayKeys(interpreter, scanKeys, { gapMs: 8, initialTime: 2000 });

    // The scan must not contain the prefix 'A'
    expect(scans).toEqual([token]);
  });

  it("heartbeatGoesStaleAfterTheWindow", () => {
    let mockTime = 1000;
    const interpreter = new HidWedgeInterpreter({
      freshnessWindowMs: DEFAULT_FRESHNESS_WINDOW_MS,
      now: () => mockTime,
    });

    expect(interpreter.isFresh()).toBe(false);
    expect(interpreter.getLastActivityAt()).toBeNull();

    // Perform a valid scan
    const keys = ["H", "D", "-", "U", "-", "7", "K", "3", "Enter"];
    for (const key of keys) {
      mockTime += 8;
      interpreter.processKey({ key, timeStamp: mockTime });
    }

    expect(interpreter.getLastActivityAt()).not.toBeNull();
    expect(interpreter.isFresh()).toBe(true);

    // Advance beyond freshness window (60s)
    mockTime += DEFAULT_FRESHNESS_WINDOW_MS + 1000;
    vi.advanceTimersByTime(DEFAULT_FRESHNESS_WINDOW_MS + 1000);
    expect(interpreter.isFresh(DEFAULT_FRESHNESS_WINDOW_MS)).toBe(false);
  });

  it("caps an unterminated buffer", () => {
    const dropped: string[] = [];
    const interpreter = new HidWedgeInterpreter({
      maxBufferLength: MAX_BUFFER_LENGTH,
      onBufferDropped: (reason) => dropped.push(reason),
    });

    // Send 70 consecutive fast characters without Enter
    const longStream = "A".repeat(MAX_BUFFER_LENGTH + 5).split("");
    replayKeys(interpreter, longStream, { gapMs: 5 });

    expect(dropped).toContain("overflow");

    // An enter now should not emit a huge garbage string
    const enterResult = interpreter.processKey({ key: "Enter", timeStamp: 2000 });
    expect(enterResult.emitted).toBe(false);
  });

  it("records up to MAX_DIAGNOSTICS_ENTRIES in memory without leaking", () => {
    const interpreter = new HidWedgeInterpreter();

    for (let i = 1; i <= 15; i++) {
      const token = `HD-U-TEST${String(i).padStart(3, "0")}`;
      const keys = [...token.split(""), "Enter"];
      replayKeys(interpreter, keys, { gapMs: 8, initialTime: i * 2000 });
    }

    const diag = interpreter.getDiagnostics();
    expect(diag.length).toBe(10);
    // Oldest entries 1-5 dropped, only last 10 preserved
    expect(diag[0].raw).toBe("HD-U-TEST006");
    expect(diag[9].raw).toBe("HD-U-TEST015");
    expect(diag[9].emitted).toBe(true);
    expect(diag[9].timings.length).toBeGreaterThan(0);

    interpreter.clearDiagnostics();
    expect(interpreter.getDiagnostics()).toEqual([]);
  });
});

describe("HidWedgeSource (DOM Integration & ScanRouter)", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("emits scans through ScanRouter and suppresses Enter only on scan", async () => {
    const router = new ScanRouter();
    const wedge = new HidWedgeSource();
    router.register(wedge);

    await router.start();

    const receivedEvents: ScanEvent[] = [];
    router.subscribe((ev) => receivedEvents.push(ev));

    // Create a mock button in DOM
    const button = document.createElement("button");
    document.body.appendChild(button);
    button.focus();

    let buttonClickCount = 0;
    button.addEventListener("click", () => {
      buttonClickCount++;
    });

    // Simulate fast scan: HD-U-B3G6822S6K-H followed by Enter
    const token = "HD-U-B3G6822S6K-H";
    let now = 1000;
    for (const char of token) {
      now += 8;
      vi.advanceTimersByTime(8);
      window.dispatchEvent(
        new KeyboardEvent("keydown", {
          key: char,
          bubbles: true,
          cancelable: true,
        })
      );
    }

    // Terminating Enter
    now += 8;
    vi.advanceTimersByTime(8);
    const enterEvent = new KeyboardEvent("keydown", {
      key: "Enter",
      bubbles: true,
      cancelable: true,
    });
    window.dispatchEvent(enterEvent);

    expect(enterEvent.defaultPrevented).toBe(true);
    expect(receivedEvents).toHaveLength(1);
    expect(receivedEvents[0]).toMatchObject({
      kind: "scan",
      scan: {
        token: "HD-U-B3G6822S6K-H",
        source: "scanner",
      },
    });

    // Verify button was not triggered
    expect(buttonClickCount).toBe(0);

    // Clean up
    await router.stop();
    document.body.removeChild(button);
  });

  it("does not suppress normal Enter key when no scan was typed", async () => {
    const router = new ScanRouter();
    const wedge = new HidWedgeSource();
    router.register(wedge);
    await router.start();

    const normalEnterEvent = new KeyboardEvent("keydown", {
      key: "Enter",
      bubbles: true,
      cancelable: true,
    });
    window.dispatchEvent(normalEnterEvent);

    expect(normalEnterEvent.defaultPrevented).toBe(false);
    await router.stop();
  });

  it("handles human typing in focused text input without emitting false scans, while allowing barcode scan", async () => {
    const router = new ScanRouter();
    const wedge = new HidWedgeSource();
    router.register(wedge);
    await router.start();

    const events: ScanEvent[] = [];
    router.subscribe((e) => events.push(e));

    const input = document.createElement("input");
    input.type = "text";
    document.body.appendChild(input);
    input.focus();

    // 1. Human typing slowly in input (120ms gap)
    const humanChars = "123456".split("");
    for (const char of humanChars) {
      vi.advanceTimersByTime(120);
      const ev = new KeyboardEvent("keydown", {
        key: char,
        bubbles: true,
        cancelable: true,
      });
      input.dispatchEvent(ev);
    }

    // Human presses Enter in input
    vi.advanceTimersByTime(120);
    const humanEnter = new KeyboardEvent("keydown", {
      key: "Enter",
      bubbles: true,
      cancelable: true,
    });
    input.dispatchEvent(humanEnter);

    // Enter in input was not suppressed by wedge
    expect(humanEnter.defaultPrevented).toBe(false);
    // No scans emitted from human typing
    expect(events.filter((e) => e.kind === "scan")).toHaveLength(0);

    // 2. Barcode scanner scans while input is focused
    const validToken = "HD-U-B3G6822S6K-H";
    for (const char of validToken) {
      vi.advanceTimersByTime(8);
      const ev = new KeyboardEvent("keydown", {
        key: char,
        bubbles: true,
        cancelable: true,
      });
      input.dispatchEvent(ev);
    }
    vi.advanceTimersByTime(8);
    const scannerEnter = new KeyboardEvent("keydown", {
      key: "Enter",
      bubbles: true,
      cancelable: true,
    });
    input.dispatchEvent(scannerEnter);

    // Scan was emitted and terminating enter suppressed
    expect(scannerEnter.defaultPrevented).toBe(true);
    const scans = events.filter((e) => e.kind === "scan");
    expect(scans).toHaveLength(1);
    expect(scans[0]).toMatchObject({
      kind: "scan",
      scan: { token: validToken, source: "scanner" },
    });

    await router.stop();
    document.body.removeChild(input);
  });
});
