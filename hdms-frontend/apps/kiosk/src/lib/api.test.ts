import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import {
  deriveIdempotencyKey,
  getCurrentScanSequence,
  getNextScanSequence,
  resetScanSequence,
  resilientFetch,
  resetKioskApiForTesting,
  abortActiveSessionRequests,
  getSessionAbortSignal,
} from "./api";
import { isKioskOffline, resetConnectivityForTesting } from "./connectivity";

describe("api library & resilience", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    resetScanSequence();
    resetKioskApiForTesting();
    resetConnectivityForTesting(false);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
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

  it("retriesNetworkErrorsThreeTimesWithBackoff", async () => {
    let callCount = 0;
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => {
      callCount += 1;
      if (callCount <= 3) {
        // Fail first 3 attempts with network error
        throw new TypeError("Failed to fetch");
      }
      return new Response(JSON.stringify({ status: "ok" }), { status: 200 });
    });

    const fetchPromise = resilientFetch("https://api.test/v1/healthz");

    // Advance through the 3 retry delays
    // Attempt 1 fails -> delay ~1000ms
    await vi.advanceTimersByTimeAsync(1500);
    // Attempt 2 fails -> delay ~2000ms
    await vi.advanceTimersByTimeAsync(2500);
    // Attempt 3 fails -> delay ~4000ms
    await vi.advanceTimersByTimeAsync(4500);

    const response = await fetchPromise;
    expect(response.status).toBe(200);
    expect(callCount).toBe(4); // 1 initial + 3 retries
    expect(isKioskOffline()).toBe(false);
  });

  it("doesNotRetryOn400", async () => {
    let callCount = 0;
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => {
      callCount += 1;
      return new Response(JSON.stringify({ type: "invalid-token-format" }), {
        status: 400,
        statusText: "Bad Request",
      });
    });

    const response = await resilientFetch("https://api.test/v1/sessions/123/scan");
    expect(response.status).toBe(400);
    // MUST NOT retry on 400
    expect(callCount).toBe(1);
  });

  it("retryReusesTheSameIdempotencyKey — ensures retries cannot duplicate loans", async () => {
    const capturedIdempotencyKeys: string[] = [];
    let callCount = 0;

    vi.spyOn(globalThis, "fetch").mockImplementation(async (req) => {
      callCount += 1;
      const request = req as Request;
      capturedIdempotencyKeys.push(request.headers.get("Idempotency-Key") ?? "");

      if (callCount < 3) {
        // Return 503 Service Unavailable for first 2 attempts
        return new Response(JSON.stringify({ type: "not-ready" }), { status: 503 });
      }
      return new Response(JSON.stringify({ outcome: { kind: "borrowed" } }), { status: 200 });
    });

    const req = new Request("https://api.test/v1/sessions/123/scan", {
      method: "POST",
      headers: {
        "Idempotency-Key": "kiosk-1:sess-abc:1",
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ token: "HD-U-12345" }),
    });

    const fetchPromise = resilientFetch(req);

    // Advance past retry backoffs
    await vi.advanceTimersByTimeAsync(2000);
    await vi.advanceTimersByTimeAsync(3000);

    const response = await fetchPromise;
    expect(response.status).toBe(200);
    expect(callCount).toBe(3);

    // Every attempt MUST carry the identical Idempotency-Key header
    expect(capturedIdempotencyKeys).toEqual([
      "kiosk-1:sess-abc:1",
      "kiosk-1:sess-abc:1",
      "kiosk-1:sess-abc:1",
    ]);
  });

  it("aborts active session requests when session ends or resets", async () => {
    const signal = getSessionAbortSignal();
    expect(signal.aborted).toBe(false);

    abortActiveSessionRequests();
    expect(signal.aborted).toBe(true);

    const nextSignal = getSessionAbortSignal();
    expect(nextSignal.aborted).toBe(false);
  });

  it("honoursRetryAfterHeaderOn429", async () => {
    let callCount = 0;
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => {
      callCount += 1;
      if (callCount === 1) {
        const headers = new Headers();
        headers.set("Retry-After", "5");
        return new Response(JSON.stringify({ type: "rate-limited" }), {
          status: 429,
          headers,
        });
      }
      return new Response(JSON.stringify({ status: "ok" }), { status: 200 });
    });

    const fetchPromise = resilientFetch("https://api.test/v1/sessions/123/scan");

    // Advance 4999ms - should still be waiting
    await vi.advanceTimersByTimeAsync(4999);
    expect(callCount).toBe(1);

    // Advance 2ms (total 5001ms) - should have retried
    await vi.advanceTimersByTimeAsync(2);
    const response = await fetchPromise;
    expect(response.status).toBe(200);
    expect(callCount).toBe(2);
  });
});
