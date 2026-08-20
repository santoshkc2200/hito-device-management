import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { HidWedgeInterpreter, DEFAULT_FRESHNESS_WINDOW_MS } from "@hdms/scan";
import { getKioskConfig, setKioskConfig, clearKioskConfig } from "./kiosk-config";

describe("Phase 3.10 — Hardware Validation Automated Suite", () => {
  beforeEach(() => {
    localStorage.clear();
    sessionStorage.clear();
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    clearKioskConfig();
  });

  describe("3.10.3 — HID Wedge Timing Constant & Telemetry Verification", () => {
    it("confirms MAX_INTERVAL_MS (35ms) accepts real hardware bursts (4-18ms) and rejects human typing", () => {
      let currentTime = 1000;
      const interpreter = new HidWedgeInterpreter({
        now: () => currentTime,
      });

      // 1. Simulate real hardware imager burst: Zebra CS6080 emitting "DEV-98765432" at 10ms intervals
      const hardwareBarcode = "DEV-98765432";
      for (const char of hardwareBarcode) {
        currentTime += 10;
        const res = interpreter.processKey({ key: char, timeStamp: currentTime });
        expect(res.emitted).toBe(false);
      }
      currentTime += 10;
      const enterRes = interpreter.processKey({ key: "Enter", timeStamp: currentTime });
      expect(enterRes.emitted).toBe(true);
      expect(enterRes.scanText).toBe(hardwareBarcode);

      // Verify diagnostics captured accurate timing data
      const diags = interpreter.getDiagnostics();
      expect(diags.length).toBe(1);
      expect(diags[0].raw).toBe(hardwareBarcode);
      expect(diags[0].emitted).toBe(true);
      expect(diags[0].timings.every((t) => t === 10)).toBe(true);

      // 2. Simulate human typist entering characters at 90ms intervals
      const humanTyping = "USER-123456";
      for (const char of humanTyping) {
        currentTime += 90;
        const res = interpreter.processKey({ key: char, timeStamp: currentTime });
        // The slow keystroke interval should cause buffer drop/timeout
        expect(res.emitted).toBe(false);
      }
      currentTime += 90;
      const humanEnter = interpreter.processKey({ key: "Enter", timeStamp: currentTime });
      expect(humanEnter.emitted).toBe(false);
      expect(humanEnter.scanText).toBeUndefined();
    });

    it("maintains an in-memory diagnostics ring buffer capped at 10 entries", () => {
      let currentTime = 1000;
      const interpreter = new HidWedgeInterpreter({
        now: () => currentTime,
      });

      // Emit 15 consecutive valid scans
      for (let i = 1; i <= 15; i++) {
        const token = `ASSET-${i.toString().padStart(4, "0")}`;
        for (const char of token) {
          currentTime += 8;
          interpreter.processKey({ key: char, timeStamp: currentTime });
        }
        currentTime += 8;
        interpreter.processKey({ key: "Enter", timeStamp: currentTime });
      }

      const diags = interpreter.getDiagnostics();
      expect(diags.length).toBe(10);
      // Last entry should be ASSET-0015
      expect(diags[diags.length - 1].raw).toBe("ASSET-0015");
      // First entry should be ASSET-0006 (earliest 5 rolled off)
      expect(diags[0].raw).toBe("ASSET-0006");
    });

    it("verifies MIN_LENGTH threshold rejects truncated scans below 6 chars", () => {
      let currentTime = 2000;
      const interpreter = new HidWedgeInterpreter({
        now: () => currentTime,
      });

      const truncated = "DEV-1"; // 5 chars
      for (const char of truncated) {
        currentTime += 12;
        interpreter.processKey({ key: char, timeStamp: currentTime });
      }
      currentTime += 12;
      const res = interpreter.processKey({ key: "Enter", timeStamp: currentTime });
      expect(res.emitted).toBe(false);

      const diags = interpreter.getDiagnostics();
      expect(diags.length).toBe(1);
      expect(diags[0].raw).toBe(truncated);
      expect(diags[0].emitted).toBe(false);
    });
  });

  describe("3.10.B — Scanner Sleep & Freshness Heartbeat", () => {
    it("transitions scanner freshness to stale after inactivity and revives on new scan", () => {
      let currentTime = 10_000;
      const interpreter = new HidWedgeInterpreter({
        freshnessWindowMs: DEFAULT_FRESHNESS_WINDOW_MS,
        now: () => currentTime,
      });

      expect(interpreter.isFresh()).toBe(false);
      expect(interpreter.getLastActivityAt()).toBeNull();

      // Perform a scan
      const scanToken = "STAFF-990011";
      for (const char of scanToken) {
        currentTime += 6;
        interpreter.processKey({ key: char, timeStamp: currentTime });
      }
      currentTime += 6;
      interpreter.processKey({ key: "Enter", timeStamp: currentTime });

      expect(interpreter.isFresh()).toBe(true);
      expect(interpreter.getLastActivityAt()).toBeTruthy();

      // Advance time past freshness window (60s)
      currentTime += DEFAULT_FRESHNESS_WINDOW_MS + 1000;
      expect(interpreter.isFresh()).toBe(false);

      // Scanner wakes up after 30 minutes sleep and emits next scan
      currentTime += 30 * 60 * 1000;
      for (const char of "DEV-11223344") {
        currentTime += 8;
        interpreter.processKey({ key: char, timeStamp: currentTime });
      }
      currentTime += 8;
      const wakeScan = interpreter.processKey({ key: "Enter", timeStamp: currentTime });
      expect(wakeScan.emitted).toBe(true);
      expect(wakeScan.scanText).toBe("DEV-11223344");
      expect(interpreter.isFresh()).toBe(true);
    });
  });

  describe("3.10.A — Storage Survival & Standalone Persistence", () => {
    it("persists kiosk pairing config in localStorage across restarts", () => {
      const config = {
        token: "kiosk_tok_hospital_sec_01",
        kioskId: "kiosk-ed-01",
        kioskName: "Emergency Dept Kiosk",
      };

      setKioskConfig(config);

      const retrieved = getKioskConfig();
      expect(retrieved).not.toBeNull();
      expect(retrieved?.token).toBe("kiosk_tok_hospital_sec_01");
      expect(retrieved?.kioskName).toBe("Emergency Dept Kiosk");

      // Verify token never appears in DOM/URL or session-only storage
      expect(sessionStorage.getItem("hdms_kiosk_config")).toBeNull();
      expect(window.location.href).not.toContain("kiosk_tok");
    });
  });
});
