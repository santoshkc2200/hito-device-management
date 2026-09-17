import { beforeEach, afterEach, describe, expect, it } from "vitest";
import {
  enqueueQueueItem,
  getQueueItems,
  getPendingQueueItems,
  getPendingCount,
  closeQueueDb,
  resetQueueDbForTesting,
  deleteQueueItem,
  getDeletionAuditLogs,
  updateQueueItemStatus,
  MAX_QUEUE_SIZE,
  QueueFullError,
} from "./offline-queue";
import { deriveIdempotencyKey, resetKioskApiForTesting, resetScanSequence } from "./api";
import { setKioskConfig, clearKioskConfig, setSessionId, clearSessionId } from "./kiosk-config";

describe("offline-queue (Deliverable 5.1a)", () => {
  const KIOSK_ID = "kiosk-ward-4";
  const SESSION_ID = "session-test-42";

  beforeEach(async () => {
    await resetQueueDbForTesting();
    clearKioskConfig();
    clearSessionId();
    resetScanSequence();

    setKioskConfig({
      kioskId: KIOSK_ID,
      kioskName: "Ward 4 Kiosk",
      token: "test-token",
    });
    setSessionId(SESSION_ID);
  });

  afterEach(async () => {
    await resetQueueDbForTesting();
    clearKioskConfig();
    clearSessionId();
  });

  it("queueSurvivesReload", async () => {
    const item1 = await enqueueQueueItem({
      request: {
        method: "POST",
        url: "/v1/sessions/scan",
        body: JSON.stringify({ barcode: "HD-D-001" }),
      },
    });

    const item2 = await enqueueQueueItem({
      request: {
        method: "POST",
        url: "/v1/sessions/scan",
        body: JSON.stringify({ barcode: "HD-D-002" }),
      },
    });

    // Simulate page reload by closing the open connection
    await closeQueueDb();

    // Reopen and retrieve items
    const restored = await getPendingQueueItems(KIOSK_ID);
    expect(restored).toHaveLength(2);
    expect(restored[0].sequence).toBe(item1.sequence);
    expect(restored[0].idempotencyKey).toBe(item1.idempotencyKey);
    expect(restored[0].request.url).toBe("/v1/sessions/scan");
    expect(restored[1].sequence).toBe(item2.sequence);
    expect(restored[1].idempotencyKey).toBe(item2.idempotencyKey);
  });

  it("queueSurvivesServiceWorkerUpdate", async () => {
    const item = await enqueueQueueItem({
      request: {
        method: "POST",
        url: "/v1/sessions/return",
        body: JSON.stringify({ deviceId: "dev-007" }),
      },
    });

    // In a service worker update, service worker caches may be flushed or replaced,
    // but the independent IndexedDB database must remain completely intact.
    await closeQueueDb();

    if (typeof caches !== "undefined") {
      const keys = await caches.keys();
      for (const k of keys) {
        await caches.delete(k);
      }
    }

    const itemsAfterSWUpdate = await getPendingQueueItems(KIOSK_ID);
    expect(itemsAfterSWUpdate).toHaveLength(1);
    expect(itemsAfterSWUpdate[0].sequence).toBe(item.sequence);
    expect(itemsAfterSWUpdate[0].idempotencyKey).toBe(item.idempotencyKey);
    expect(itemsAfterSWUpdate[0].status).toBe("pending");
  });

  it("enqueueOrderIsPreservedAcrossRestarts", async () => {
    const totalItems = 5;
    const enqueuedKeys: string[] = [];

    for (let i = 1; i <= totalItems; i++) {
      const item = await enqueueQueueItem({
        request: {
          method: "POST",
          url: `/v1/sessions/scan/${i}`,
          body: JSON.stringify({ index: i }),
        },
      });
      enqueuedKeys.push(item.idempotencyKey);
    }

    // Simulate restart
    await closeQueueDb();

    const items = await getPendingQueueItems(KIOSK_ID);
    expect(items).toHaveLength(totalItems);

    for (let i = 0; i < totalItems; i++) {
      expect(items[i].idempotencyKey).toBe(enqueuedKeys[i]);
      expect(items[i].sequence).toBe(i + 1);
    }

    // Ensure sequences are strictly monotonic
    for (let i = 0; i < items.length - 1; i++) {
      expect(items[i].sequence).toBeLessThan(items[i + 1].sequence);
    }
  });

  it("queueFullSurfacesItsOwnState", async () => {
    // Fill the queue up to MAX_QUEUE_SIZE
    for (let i = 0; i < MAX_QUEUE_SIZE; i++) {
      await enqueueQueueItem({
        request: {
          method: "POST",
          url: `/v1/sessions/scan`,
          body: JSON.stringify({ i }),
        },
      });
    }

    expect(await getPendingCount(KIOSK_ID)).toBe(MAX_QUEUE_SIZE);

    // The 201st enqueue must raise QueueFullError and not silently truncate
    await expect(
      enqueueQueueItem({
        request: {
          method: "POST",
          url: "/v1/sessions/scan",
          body: JSON.stringify({ overflow: true }),
        },
      })
    ).rejects.toThrow(QueueFullError);

    // Verify error code and details
    try {
      await enqueueQueueItem({
        request: {
          method: "POST",
          url: "/v1/sessions/scan",
        },
      });
      expect.unreachable("Should have thrown QueueFullError");
    } catch (err) {
      expect(err).toBeInstanceOf(QueueFullError);
      expect((err as QueueFullError).code).toBe("QUEUE_FULL");
    }

    // Ensure no silent truncation occurred: all MAX_QUEUE_SIZE items remain
    const pendingItems = await getPendingQueueItems(KIOSK_ID);
    expect(pendingItems).toHaveLength(MAX_QUEUE_SIZE);
  });

  it("itemsCarryTheDerivedIdempotencyKeyNotAFreshUuid", async () => {
    const item1 = await enqueueQueueItem({
      request: {
        method: "POST",
        url: "/v1/sessions/scan",
        body: JSON.stringify({ scan: "HD-D-101" }),
      },
    });

    const item2 = await enqueueQueueItem({
      request: {
        method: "POST",
        url: "/v1/sessions/scan",
        body: JSON.stringify({ scan: "HD-D-102" }),
      },
    });

    const offlineSession = `${SESSION_ID}#offline`;
    const expectedKey1 = deriveIdempotencyKey(KIOSK_ID, offlineSession, item1.sequence);
    const expectedKey2 = deriveIdempotencyKey(KIOSK_ID, offlineSession, item2.sequence);

    expect(item1.idempotencyKey).toBe(expectedKey1);
    expect(item2.idempotencyKey).toBe(expectedKey2);
    expect(item1.request.headers?.["Idempotency-Key"]).toBe(expectedKey1);
    expect(item2.request.headers?.["Idempotency-Key"]).toBe(expectedKey2);

    // Must NOT be a fresh UUID
    const uuidRegex = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
    expect(item1.idempotencyKey).not.toMatch(uuidRegex);
    expect(item2.idempotencyKey).not.toMatch(uuidRegex);

    // Format matches kiosk:session#offline:<durable store sequence>
    expect(item1.idempotencyKey).toBe(`${KIOSK_ID}:${SESSION_ID}#offline:${item1.sequence}`);
    expect(item2.idempotencyKey).toBe(`${KIOSK_ID}:${SESSION_ID}#offline:${item2.sequence}`);
    expect(item2.sequence).toBeGreaterThan(item1.sequence);
  });

  it("scopes queue reads and enqueues to the paired kioskId", async () => {
    await enqueueQueueItem({
      kioskId: "kiosk-A",
      request: { method: "POST", url: "/v1/scan", body: "a" },
    });

    await enqueueQueueItem({
      kioskId: "kiosk-B",
      request: { method: "POST", url: "/v1/scan", body: "b" },
    });

    const itemsA = await getPendingQueueItems("kiosk-A");
    expect(itemsA).toHaveLength(1);
    expect(itemsA[0].kioskId).toBe("kiosk-A");

    const itemsB = await getPendingQueueItems("kiosk-B");
    expect(itemsB).toHaveLength(1);
    expect(itemsB[0].kioskId).toBe("kiosk-B");
  });

  it("requires a reason to delete an item and writes to audit log", async () => {
    const item = await enqueueQueueItem({
      request: { method: "POST", url: "/v1/scan" },
    });

    // Deleting without reason throws
    await expect(deleteQueueItem(item.sequence, "")).rejects.toThrow(
      /Cannot delete queue item without recording a reason/
    );

    // Deleting with reason succeeds and leaves an audit trail
    const reason = "Replayed successfully and confirmed by server";
    await deleteQueueItem(item.sequence, reason);

    const remaining = await getPendingQueueItems(KIOSK_ID);
    expect(remaining).toHaveLength(0);

    const audits = await getDeletionAuditLogs(KIOSK_ID);
    expect(audits).toHaveLength(1);
    expect(audits[0].sequence).toBe(item.sequence);
    expect(audits[0].reason).toBe(reason);
    expect(audits[0].kioskId).toBe(KIOSK_ID);
    expect(audits[0].idempotencyKey).toBe(item.idempotencyKey);
  });

  it("updates item status to quarantined or done with reason", async () => {
    const item = await enqueueQueueItem({
      request: { method: "POST", url: "/v1/scan" },
    });

    const updated = await updateQueueItemStatus(
      item.sequence,
      "quarantined",
      "HTTP 422: Device already on loan"
    );

    expect(updated.status).toBe("quarantined");
    expect(updated.quarantineReason).toBe("HTTP 422: Device already on loan");

    const pending = await getPendingQueueItems(KIOSK_ID);
    expect(pending).toHaveLength(0);

    const quarantined = await getQueueItems(KIOSK_ID, "quarantined");
    expect(quarantined).toHaveLength(1);
    expect(quarantined[0].quarantineReason).toBe("HTTP 422: Device already on loan");
  });

  it("distinctItemsKeepDistinctKeysAcrossAReload", async () => {
    // A reload resets the in-memory scan counter but keeps the session id, so a
    // key derived from that counter would repeat and the server would collapse
    // the second, genuinely distinct, transaction. The durable store sequence
    // must be what the key rides on.
    resetKioskApiForTesting();
    const first = await enqueueQueueItem({
      request: { method: "POST", url: "/v1/loans", body: '{"deviceId":"d-1"}' },
    });

    resetKioskApiForTesting(); // reload: scan counter back to zero, session kept

    const second = await enqueueQueueItem({
      request: { method: "POST", url: "/v1/loans", body: '{"deviceId":"d-1"}' },
    });

    expect(second.idempotencyKey).not.toBe(first.idempotencyKey);
    expect(second.sequence).toBeGreaterThan(first.sequence);
    expect(second.request.headers?.["Idempotency-Key"]).toBe(second.idempotencyKey);

    const stored = await getQueueItems(KIOSK_ID);
    expect(stored).toHaveLength(2);
    expect(new Set(stored.map((i) => i.idempotencyKey)).size).toBe(2);
    for (const item of stored) {
      expect(item.idempotencyKey).not.toBe("");
    }
  });

  it("offlineKeysCannotCollideWithOnlineScanKeys", async () => {
    const item = await enqueueQueueItem({
      request: { method: "POST", url: "/v1/loans" },
    });
    // The online path derives kioskId:sessionId:<scan sequence>; an offline key
    // must never land in that same namespace.
    expect(item.idempotencyKey).not.toBe(
      deriveIdempotencyKey(KIOSK_ID, SESSION_ID, item.sequence)
    );
    expect(item.idempotencyKey).toContain("#offline");
  });
});
