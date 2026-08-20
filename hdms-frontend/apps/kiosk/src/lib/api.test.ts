import { beforeEach, describe, expect, it } from "vitest";
import {
  deriveIdempotencyKey,
  getCurrentScanSequence,
  getNextScanSequence,
  resetScanSequence,
} from "./api";

describe("api library", () => {
  beforeEach(() => {
    resetScanSequence();
  });

  it("idempotency key is stable across retries of the same scan", () => {
    const kioskId = "kiosk-ward-3";
    const sessionId = "session-0192f3c1";
    const seq = 1;

    const key1 = deriveIdempotencyKey(kioskId, sessionId, seq);
    const key2 = deriveIdempotencyKey(kioskId, sessionId, seq);

    expect(key1).toBe(key2);
    expect(key1).toBe("kiosk-ward-3:session-0192f3c1:1");
  });

  it("idempotency key differs across two sequential scans", () => {
    const kioskId = "kiosk-ward-3";
    const sessionId = "session-0192f3c1";

    const seq1 = getNextScanSequence();
    const key1 = deriveIdempotencyKey(kioskId, sessionId, seq1);

    const seq2 = getNextScanSequence();
    const key2 = deriveIdempotencyKey(kioskId, sessionId, seq2);

    expect(key1).not.toBe(key2);
    expect(key1).toBe("kiosk-ward-3:session-0192f3c1:1");
    expect(key2).toBe("kiosk-ward-3:session-0192f3c1:2");
  });

  it("sequence management increments and resets correctly", () => {
    expect(getCurrentScanSequence()).toBe(0);
    expect(getNextScanSequence()).toBe(1);
    expect(getNextScanSequence()).toBe(2);
    expect(getCurrentScanSequence()).toBe(2);
    resetScanSequence();
    expect(getCurrentScanSequence()).toBe(0);
  });
});
