import { openDB, type DBSchema, type IDBPDatabase } from "idb";
import { deriveIdempotencyKey } from "./api";
import { getKioskConfig, getSessionId } from "./kiosk-config";

export const MAX_QUEUE_SIZE = 200;
export const DB_NAME = "hdms_offline_queue";
export const DB_VERSION = 2;
export const QUEUE_STORE = "offline_queue";
export const AUDIT_STORE = "queue_audit";
export const LEASE_STORE = "replay_lease";

export type QueueItemStatus = "pending" | "quarantined" | "done";

export interface QueueRequest {
  method: string;
  url: string;
  body?: string | null;
  headers?: Record<string, string>;
}

export interface QueueItem {
  sequence: number;
  kioskId: string;
  idempotencyKey: string;
  request: QueueRequest;
  enqueuedAt: number;
  attemptCount: number;
  status: QueueItemStatus;
  quarantineReason?: string;
  serverOutcome?: unknown;
  replayedAt?: number;
}

export interface QueueDeletionAudit {
  id?: number;
  sequence: number;
  kioskId: string;
  idempotencyKey: string;
  deletedAt: number;
  reason: string;
}

export interface ReplayLease {
  kioskId: string;
  holderId: string;
  acquiredAt: number;
  expiresAt: number;
}

export interface OfflineQueueDBSchema extends DBSchema {
  [QUEUE_STORE]: {
    key: number;
    value: QueueItem;
    indexes: {
      by_kiosk: string;
      by_kiosk_status: [string, QueueItemStatus];
    };
  };
  [AUDIT_STORE]: {
    key: number;
    value: QueueDeletionAudit;
    indexes: {
      by_kiosk: string;
    };
  };
  [LEASE_STORE]: {
    key: string;
    value: ReplayLease;
  };
}

export class QueueFullError extends Error {
  readonly code = "QUEUE_FULL" as const;
  readonly maxQueueSize: number = MAX_QUEUE_SIZE;

  constructor(
    message = `Offline queue is full: maximum capacity of ${MAX_QUEUE_SIZE} pending items reached`
  ) {
    super(message);
    this.name = "QueueFullError";
    Object.setPrototypeOf(this, QueueFullError.prototype);
  }
}

let dbPromise: Promise<IDBPDatabase<OfflineQueueDBSchema>> | null = null;

export function getQueueDb(): Promise<IDBPDatabase<OfflineQueueDBSchema>> {
  if (!dbPromise) {
    dbPromise = openDB<OfflineQueueDBSchema>(DB_NAME, DB_VERSION, {
      upgrade(db) {
        if (!db.objectStoreNames.contains(QUEUE_STORE)) {
          const queueStore = db.createObjectStore(QUEUE_STORE, {
            keyPath: "sequence",
            autoIncrement: true,
          });
          queueStore.createIndex("by_kiosk", "kioskId");
          queueStore.createIndex("by_kiosk_status", ["kioskId", "status"]);
        }
        if (!db.objectStoreNames.contains(AUDIT_STORE)) {
          const auditStore = db.createObjectStore(AUDIT_STORE, {
            keyPath: "id",
            autoIncrement: true,
          });
          auditStore.createIndex("by_kiosk", "kioskId");
        }
        if (!db.objectStoreNames.contains(LEASE_STORE)) {
          db.createObjectStore(LEASE_STORE, {
            keyPath: "kioskId",
          });
        }
      },
    });
  }
  return dbPromise;
}

export async function closeQueueDb(): Promise<void> {
  if (dbPromise) {
    const db = await dbPromise;
    db.close();
    dbPromise = null;
  }
}

export async function resetQueueDbForTesting(): Promise<void> {
  await closeQueueDb();
  if (typeof indexedDB !== "undefined") {
    await new Promise<void>((resolve, reject) => {
      const req = indexedDB.deleteDatabase(DB_NAME);
      req.onsuccess = () => resolve();
      req.onerror = () => reject(req.error);
      req.onblocked = () => resolve();
    });
  }
}

export interface EnqueueOptions {
  request: QueueRequest;
  kioskId?: string;
  sessionId?: string;
  scanSequence?: number;
  idempotencyKey?: string;
}

export async function enqueueQueueItem(options: EnqueueOptions): Promise<QueueItem> {
  const kioskId = options.kioskId ?? getKioskConfig()?.kioskId;
  if (!kioskId) {
    throw new Error("Cannot enqueue offline item without a paired kiosk ID");
  }

  const db = await getQueueDb();
  const tx = db.transaction(QUEUE_STORE, "readwrite");
  const kioskStatusIndex = tx.store.index("by_kiosk_status");
  const pendingCount = await kioskStatusIndex.count(IDBKeyRange.only([kioskId, "pending"]));

  if (pendingCount >= MAX_QUEUE_SIZE) {
    await tx.done;
    throw new QueueFullError();
  }

  const sessionId = options.sessionId ?? getSessionId() ?? "offline-session";

  const itemToInsert: Omit<QueueItem, "sequence"> = {
    kioskId,
    idempotencyKey: "",
    request: { ...options.request },
    enqueuedAt: Date.now(),
    attemptCount: 0,
    status: "pending",
  };

  // The key is derived from the store's own autoincrement sequence, which is
  // the only counter here that survives a reload, a service-worker update and
  // a power cycle. An in-memory scan counter restarts at zero on reload, so
  // two genuinely distinct offline transactions would derive the same key and
  // the server would collapse the second one. Hence: insert first to claim the
  // durable sequence, then write the derived key back inside the same
  // transaction, so no item is ever visible without its key.
  const sequence = (await tx.store.add(itemToInsert as QueueItem)) as number;

  const idempotencyKey =
    options.idempotencyKey ??
    deriveIdempotencyKey(
      kioskId,
      `${sessionId}#offline`,
      options.scanSequence ?? sequence
    );

  const headers: Record<string, string> = {
    ...(options.request.headers ?? {}),
    "Idempotency-Key": idempotencyKey,
  };

  const item: QueueItem = {
    ...itemToInsert,
    sequence,
    idempotencyKey,
    request: { ...options.request, headers },
  };

  await tx.store.put(item);
  await tx.done;

  notifyQueueChange();
  return item;
}

export async function getQueueItems(
  kioskId?: string,
  status?: QueueItemStatus
): Promise<QueueItem[]> {
  const resolvedKioskId = kioskId ?? getKioskConfig()?.kioskId;
  if (!resolvedKioskId) return [];

  const db = await getQueueDb();
  const tx = db.transaction(QUEUE_STORE, "readonly");

  let items: QueueItem[];
  if (status) {
    const index = tx.store.index("by_kiosk_status");
    items = await index.getAll(IDBKeyRange.only([resolvedKioskId, status]));
  } else {
    const index = tx.store.index("by_kiosk");
    items = await index.getAll(IDBKeyRange.only(resolvedKioskId));
  }

  return items.sort((a, b) => a.sequence - b.sequence);
}

export async function getPendingQueueItems(kioskId?: string): Promise<QueueItem[]> {
  return getQueueItems(kioskId, "pending");
}

export async function getPendingCount(kioskId?: string): Promise<number> {
  const resolvedKioskId = kioskId ?? getKioskConfig()?.kioskId;
  if (!resolvedKioskId) return 0;

  const db = await getQueueDb();
  const tx = db.transaction(QUEUE_STORE, "readonly");
  const index = tx.store.index("by_kiosk_status");
  return index.count(IDBKeyRange.only([resolvedKioskId, "pending"]));
}

export async function getQueueItemBySequence(sequence: number): Promise<QueueItem | undefined> {
  const db = await getQueueDb();
  return db.get(QUEUE_STORE, sequence);
}

export async function updateQueueItemStatus(
  sequence: number,
  status: QueueItemStatus,
  quarantineReason?: string,
  serverOutcome?: unknown
): Promise<QueueItem> {
  const db = await getQueueDb();
  const tx = db.transaction(QUEUE_STORE, "readwrite");
  const item = await tx.store.get(sequence);
  if (!item) {
    await tx.done;
    throw new Error(`Queue item with sequence ${sequence} not found`);
  }
  item.status = status;
  if (quarantineReason !== undefined) {
    item.quarantineReason = quarantineReason;
  }
  if (serverOutcome !== undefined) {
    item.serverOutcome = serverOutcome;
  }
  if (status === "done" || status === "quarantined") {
    item.replayedAt = Date.now();
  }
  await tx.store.put(item);
  await tx.done;

  notifyQueueChange();
  return item;
}

export async function incrementAttemptCount(sequence: number): Promise<QueueItem> {
  const db = await getQueueDb();
  const tx = db.transaction(QUEUE_STORE, "readwrite");
  const item = await tx.store.get(sequence);
  if (!item) {
    await tx.done;
    throw new Error(`Queue item with sequence ${sequence} not found`);
  }
  item.attemptCount += 1;
  await tx.store.put(item);
  await tx.done;

  notifyQueueChange();
  return item;
}

export async function deleteQueueItem(sequence: number, reason: string): Promise<void> {
  if (!reason || !reason.trim()) {
    throw new Error("Cannot delete queue item without recording a reason");
  }
  const db = await getQueueDb();
  const tx = db.transaction([QUEUE_STORE, AUDIT_STORE], "readwrite");
  const queueStore = tx.objectStore(QUEUE_STORE);
  const auditStore = tx.objectStore(AUDIT_STORE);

  const existing = await queueStore.get(sequence);
  if (!existing) {
    await tx.done;
    return;
  }

  const auditRecord: QueueDeletionAudit = {
    sequence,
    kioskId: existing.kioskId,
    idempotencyKey: existing.idempotencyKey,
    deletedAt: Date.now(),
    reason: reason.trim(),
  };

  await auditStore.add(auditRecord);
  await queueStore.delete(sequence);
  await tx.done;

  notifyQueueChange();
}

export async function clearQueue(reason: string, kioskId?: string): Promise<number> {
  if (!reason || !reason.trim()) {
    throw new Error("Cannot clear queue without recording a reason");
  }
  const db = await getQueueDb();
  const tx = db.transaction([QUEUE_STORE, AUDIT_STORE], "readwrite");
  const queueStore = tx.objectStore(QUEUE_STORE);
  const auditStore = tx.objectStore(AUDIT_STORE);

  let items: QueueItem[];
  if (kioskId) {
    const index = queueStore.index("by_kiosk");
    items = await index.getAll(IDBKeyRange.only(kioskId));
  } else {
    items = await queueStore.getAll();
  }

  for (const item of items) {
    const auditRecord: QueueDeletionAudit = {
      sequence: item.sequence,
      kioskId: item.kioskId,
      idempotencyKey: item.idempotencyKey,
      deletedAt: Date.now(),
      reason: reason.trim(),
    };
    await auditStore.add(auditRecord);
    await queueStore.delete(item.sequence);
  }

  await tx.done;

  notifyQueueChange();
  return items.length;
}

export async function getDeletionAuditLogs(kioskId?: string): Promise<QueueDeletionAudit[]> {
  const db = await getQueueDb();
  const tx = db.transaction(AUDIT_STORE, "readonly");
  if (kioskId) {
    const index = tx.store.index("by_kiosk");
    return index.getAll(IDBKeyRange.only(kioskId));
  }
  return tx.store.getAll();
}

export async function acquireReplayLease(
  kioskId: string,
  holderId: string,
  durationMs = 15_000,
  now = Date.now()
): Promise<boolean> {
  const db = await getQueueDb();
  const tx = db.transaction(LEASE_STORE, "readwrite");
  const existing = await tx.store.get(kioskId);

  if (existing && existing.expiresAt > now && existing.holderId !== holderId) {
    await tx.done;
    return false;
  }

  const lease: ReplayLease = {
    kioskId,
    holderId,
    acquiredAt: existing && existing.holderId === holderId ? existing.acquiredAt : now,
    expiresAt: now + durationMs,
  };

  await tx.store.put(lease);
  await tx.done;
  return true;
}

export async function renewReplayLease(
  kioskId: string,
  holderId: string,
  durationMs = 15_000,
  now = Date.now()
): Promise<boolean> {
  const db = await getQueueDb();
  const tx = db.transaction(LEASE_STORE, "readwrite");
  const existing = await tx.store.get(kioskId);

  if (!existing || existing.holderId !== holderId) {
    await tx.done;
    return false;
  }

  existing.expiresAt = now + durationMs;
  await tx.store.put(existing);
  await tx.done;
  return true;
}

export async function releaseReplayLease(
  kioskId: string,
  holderId: string
): Promise<void> {
  const db = await getQueueDb();
  const tx = db.transaction(LEASE_STORE, "readwrite");
  const existing = await tx.store.get(kioskId);

  if (existing && existing.holderId === holderId) {
    await tx.store.delete(kioskId);
  }
  await tx.done;
}

export async function getReplayLease(
  kioskId: string
): Promise<ReplayLease | undefined> {
  const db = await getQueueDb();
  return db.get(LEASE_STORE, kioskId);
}

type QueueListener = () => void;
const queueListeners = new Set<QueueListener>();

export function subscribeQueue(listener: QueueListener): () => void {
  queueListeners.add(listener);
  return () => {
    queueListeners.delete(listener);
  };
}

export function notifyQueueChange(): void {
  for (const listener of queueListeners) {
    try {
      listener();
    } catch {
      // Ignore listener callback exceptions
    }
  }
}
