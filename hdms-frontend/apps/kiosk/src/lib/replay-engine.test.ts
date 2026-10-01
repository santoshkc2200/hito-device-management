import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import {
  enqueueQueueItem,
  getQueueItems,
  getPendingQueueItems,
  getPendingCount,
  resetQueueDbForTesting,
} from "./offline-queue";
import { setKioskConfig, clearKioskConfig, setSessionId, clearSessionId } from "./kiosk-config";
import { resetConnectivityForTesting } from "./connectivity";
import {
  replayQueue,
  acquireReplayLease,
  releaseReplayLease,
  getReplayLease,
} from "./replay-engine";

describe("5.1c Replay Engine", () => {
  const KIOSK_ID = "kiosk-ward-replay";
  const SESSION_ID = "session-replay-101";

  beforeEach(async () => {
    await resetQueueDbForTesting();
    clearKioskConfig();
    clearSessionId();
    resetConnectivityForTesting(false);

    setKioskConfig({
      kioskId: KIOSK_ID,
      kioskName: "Ward Replay Kiosk",
      token: "test-replay-bearer-token",
    });
    setSessionId(SESSION_ID);
  });

  afterEach(async () => {
    await resetQueueDbForTesting();
    clearKioskConfig();
    clearSessionId();
    vi.restoreAllMocks();
  });

  it("replayIsOrderedPerDevice", async () => {
    // Enqueue 3 operations on devices:
    // 1. Return Device A
    // 2. Borrow Device A
    // 3. Return Device B
    const item1 = await enqueueQueueItem({
      kioskId: KIOSK_ID,
      request: {
        method: "POST",
        url: "/v1/sessions/return",
        body: JSON.stringify({ deviceId: "DEV-A", action: "return" }),
      },
    });

    const item2 = await enqueueQueueItem({
      kioskId: KIOSK_ID,
      request: {
        method: "POST",
        url: "/v1/sessions/borrow",
        body: JSON.stringify({ deviceId: "DEV-A", action: "borrow" }),
      },
    });

    const item3 = await enqueueQueueItem({
      kioskId: KIOSK_ID,
      request: {
        method: "POST",
        url: "/v1/sessions/return",
        body: JSON.stringify({ deviceId: "DEV-B", action: "return" }),
      },
    });

    expect(item1.sequence).toBeLessThan(item2.sequence);
    expect(item2.sequence).toBeLessThan(item3.sequence);

    // Track execution order
    const executedSequences: number[] = [];
    const mockFetch = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      const body = JSON.parse((init?.body as string) || "{}");
      if (body.deviceId === "DEV-A" && body.action === "return") {
        executedSequences.push(1);
      } else if (body.deviceId === "DEV-A" && body.action === "borrow") {
        executedSequences.push(2);
      } else if (body.deviceId === "DEV-B" && body.action === "return") {
        executedSequences.push(3);
      }
      return new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });

    const result = await replayQueue({
      kioskId: KIOSK_ID,
      fetchFn: mockFetch as any,
    });

    expect(result.succeeded).toBe(3);
    expect(result.remaining).toBe(0);
    // Must replay strictly in enqueue order: 1 -> 2 -> 3
    expect(executedSequences).toEqual([1, 2, 3]);

    const pending = await getPendingQueueItems(KIOSK_ID);
    expect(pending).toHaveLength(0);
  });

  it("crashMidFlightReplaysAndServerCollapsesTheDuplicate", async () => {
    // 1. Enqueue an item with its deterministic derived key
    const item = await enqueueQueueItem({
      kioskId: KIOSK_ID,
      request: {
        method: "POST",
        url: "/v1/sessions/scan",
        body: JSON.stringify({ barcode: "HD-D-CRASH-TEST" }),
      },
    });

    const originalIdempotencyKey = item.idempotencyKey;
    expect(originalIdempotencyKey).toBeTruthy();

    // Simulated server database tracking received idempotency keys
    const serverReceivedKeys: string[] = [];
    let serverExecutionCount = 0;

    const mockServerFetch = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      const headers = new Headers(init?.headers);
      const key = headers.get("Idempotency-Key") || "";
      serverReceivedKeys.push(key);

      if (serverReceivedKeys.filter((k) => k === key).length === 1) {
        // First attempt recorded server side
        serverExecutionCount += 1;
      } else {
        // Server detects duplicate idempotency key -> collapses duplicate without re-executing
      }

      return new Response(
        JSON.stringify({
          loanId: "loan-collapsed-123",
          state: "ready",
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }
      );
    });

    // Attempt 1: Replay initiates, server receives and processes request,
    // but client simulates a crash mid-flight before persisting outcome in IndexedDB.
    await mockServerFetch(item.request.url, {
      method: item.request.method,
      headers: item.request.headers,
      body: item.request.body,
    });

    expect(serverExecutionCount).toBe(1);
    expect(serverReceivedKeys).toHaveLength(1);

    // After crash/restart, item is still pending in queue
    const stillPending = await getPendingQueueItems(KIOSK_ID);
    expect(stillPending).toHaveLength(1);

    // Attempt 2: Replay engine runs on restart.
    // It MUST reuse the existing idempotency key stored on the item, never re-deriving a new key.
    const result = await replayQueue({
      kioskId: KIOSK_ID,
      fetchFn: mockServerFetch as any,
    });

    expect(result.succeeded).toBe(1);
    expect(result.remaining).toBe(0);

    // The server saw two requests with the EXACT SAME key
    expect(serverReceivedKeys).toHaveLength(2);
    expect(serverReceivedKeys[0]).toBe(originalIdempotencyKey);
    expect(serverReceivedKeys[1]).toBe(originalIdempotencyKey);

    // Server collapsed the duplicate: execution count is still 1
    expect(serverExecutionCount).toBe(1);

    // Succeeded item has recorded server outcome
    const doneItems = await getQueueItems(KIOSK_ID, "done");
    expect(doneItems).toHaveLength(1);
    expect(doneItems[0].serverOutcome).toEqual({
      loanId: "loan-collapsed-123",
      state: "ready",
    });
  });

  it("concurrentReplayIsPreventedBySingleFlight", async () => {
    await enqueueQueueItem({
      kioskId: KIOSK_ID,
      request: {
        method: "POST",
        url: "/v1/sessions/scan",
        body: JSON.stringify({ barcode: "HD-D-CONCURRENT" }),
      },
    });

    // Tab 1 acquires durable lease in IndexedDB
    const tab1HolderId = "tab-1-uuid";
    const acquired = await acquireReplayLease(KIOSK_ID, tab1HolderId, 10000);
    expect(acquired).toBe(true);

    const lease = await getReplayLease(KIOSK_ID);
    expect(lease).toBeDefined();
    expect(lease?.holderId).toBe(tab1HolderId);

    // Tab 2 attempts replay while Tab 1's durable lease is active
    const tab2HolderId = "tab-2-uuid";
    const tab2Result = await replayQueue({
      kioskId: KIOSK_ID,
      holderId: tab2HolderId,
      fetchFn: vi.fn() as any,
    });

    // Tab 2 must be locked out by single-flight protection
    expect(tab2Result.stoppedReason).toBe("locked");
    expect(tab2Result.succeeded).toBe(0);
    expect(tab2Result.remaining).toBe(1);

    // Tab 1 finishes and releases lease
    await releaseReplayLease(KIOSK_ID, tab1HolderId);
    expect(await getReplayLease(KIOSK_ID)).toBeUndefined();

    // Now Tab 2 can acquire lease and replay
    const mockFetch = vi.fn(async () => new Response(JSON.stringify({ ok: true }), { status: 200 }));
    const retryResult = await replayQueue({
      kioskId: KIOSK_ID,
      holderId: tab2HolderId,
      fetchFn: mockFetch as any,
    });

    expect(retryResult.stoppedReason).toBeUndefined();
    expect(retryResult.succeeded).toBe(1);
    expect(retryResult.remaining).toBe(0);
  });

  it("permanentFailureQuarantinesWithReason", async () => {
    // Server rejects item: device already on loan (422 RFC 9457 problem)
    await enqueueQueueItem({
      kioskId: KIOSK_ID,
      request: {
        method: "POST",
        url: "/v1/sessions/borrow",
        body: JSON.stringify({ deviceId: "dev-conflict-99" }),
      },
    });

    const mockFetch = vi.fn(async () => {
      return new Response(
        JSON.stringify({
          type: "https://hdms.hospital.local/problems/device-on-loan",
          title: "Device Already on Loan",
          status: 422,
          detail: "This device is currently borrowed by Dr. Chen.",
          supportCode: "SUP-422-CONFLICT",
        }),
        {
          status: 422,
          headers: { "Content-Type": "application/problem+json" },
        }
      );
    });

    const result = await replayQueue({
      kioskId: KIOSK_ID,
      fetchFn: mockFetch as any,
    });

    expect(result.succeeded).toBe(0);
    expect(result.quarantined).toBe(1);
    expect(result.remaining).toBe(0);

    // The item must be in quarantine status, NOT silently dropped or retried forever
    const quarantinedItems = await getQueueItems(KIOSK_ID, "quarantined");
    expect(quarantinedItems).toHaveLength(1);
    expect(quarantinedItems[0].status).toBe("quarantined");
    expect(quarantinedItems[0].quarantineReason).toContain("This device is currently borrowed by Dr. Chen.");
    expect(quarantinedItems[0].serverOutcome).toBeDefined();

    // Pending queue is empty
    expect(await getPendingCount(KIOSK_ID)).toBe(0);
  });

  it("replayStopsWhenConnectivityDropsAgainAndResumesInOrder", async () => {
    await enqueueQueueItem({
      kioskId: KIOSK_ID,
      request: { method: "POST", url: "/v1/item/1", body: JSON.stringify({ seq: 1 }) },
    });
    const item2 = await enqueueQueueItem({
      kioskId: KIOSK_ID,
      request: { method: "POST", url: "/v1/item/2", body: JSON.stringify({ seq: 2 }) },
    });
    const item3 = await enqueueQueueItem({
      kioskId: KIOSK_ID,
      request: { method: "POST", url: "/v1/item/3", body: JSON.stringify({ seq: 3 }) },
    });

    let networkOnline = true;
    const replayedSequences: number[] = [];

    const mockFetch = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      const body = JSON.parse((init?.body as string) || "{}");
      if (!networkOnline) {
        throw new TypeError("Failed to fetch: network unreachable");
      }
      if (body.seq === 1) {
        replayedSequences.push(1);
        // Network drops immediately after item 1 completes
        networkOnline = false;
        return new Response(JSON.stringify({ ok: true }), { status: 200 });
      }
      replayedSequences.push(body.seq);
      return new Response(JSON.stringify({ ok: true }), { status: 200 });
    });

    // First replay run: item 1 succeeds, item 2 fails because network dropped
    const pass1 = await replayQueue({
      kioskId: KIOSK_ID,
      fetchFn: mockFetch as any,
      isOfflineFn: () => !networkOnline,
    });

    expect(pass1.succeeded).toBe(1);
    expect(pass1.stoppedReason).toBe("offline");
    // Item 1 succeeded, item 2 and 3 remain pending
    expect(replayedSequences).toEqual([1]);

    const pendingMidway = await getPendingQueueItems(KIOSK_ID);
    expect(pendingMidway).toHaveLength(2);
    expect(pendingMidway[0].sequence).toBe(item2.sequence);
    expect(pendingMidway[1].sequence).toBe(item3.sequence);

    // Network is restored
    networkOnline = true;

    // Second replay run resumes in order
    const pass2 = await replayQueue({
      kioskId: KIOSK_ID,
      fetchFn: mockFetch as any,
      isOfflineFn: () => !networkOnline,
    });

    expect(pass2.succeeded).toBe(2);
    expect(pass2.remaining).toBe(0);
    expect(replayedSequences).toEqual([1, 2, 3]);

    const finalPending = await getPendingCount(KIOSK_ID);
    expect(finalPending).toBe(0);
  });

  it("holdsQueuedItemsWhileTheServerIsUnderMaintenance", async () => {
    await enqueueQueueItem({
      kioskId: KIOSK_ID,
      request: {
        method: "POST",
        url: "/v1/sessions/return",
        body: JSON.stringify({ deviceId: "DEV-M", action: "return" }),
      },
    });
    const mockFetch = vi.fn(async () =>
      new Response(
        JSON.stringify({ type: "https://hdms.hito.local/errors/maintenance", title: "Under maintenance", status: 503 }),
        { status: 503, headers: { "Content-Type": "application/problem+json" } },
      ),
    );

    const result = await replayQueue({ kioskId: KIOSK_ID, fetchFn: mockFetch as any });

    expect(result.quarantined).toBe(0);
    expect(result.succeeded).toBe(0);
    expect(result.remaining).toBe(1);
    expect(await getPendingQueueItems(KIOSK_ID)).toHaveLength(1);
  });
});

