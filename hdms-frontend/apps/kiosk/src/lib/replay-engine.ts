import {
  acquireReplayLease,
  renewReplayLease,
  releaseReplayLease,
  getReplayLease,
  getPendingQueueItems,
  getPendingCount,
  updateQueueItemStatus,
  incrementAttemptCount,
  type QueueItem,
} from "./offline-queue";
import { getKioskConfig } from "./kiosk-config";
import { isKioskOffline, onKioskReconnect } from "./connectivity";
import { parseProblem } from "./problem";
import { resilientFetch } from "./api";

export {
  acquireReplayLease,
  renewReplayLease,
  releaseReplayLease,
  getReplayLease,
};

export interface ReplayResult {
  succeeded: number;
  quarantined: number;
  remaining: number;
  stoppedReason?: "offline" | "locked" | "empty" | "error";
}

export interface ReplayOptions {
  kioskId?: string;
  holderId?: string;
  leaseDurationMs?: number;
  fetchFn?: typeof fetch;
  isOfflineFn?: () => boolean;
  onItemReplayed?: (item: QueueItem, outcome: "succeeded" | "quarantined") => void;
}

export async function replayQueue(options?: ReplayOptions): Promise<ReplayResult> {
  const kioskId = options?.kioskId ?? getKioskConfig()?.kioskId;
  if (!kioskId) {
    return {
      succeeded: 0,
      quarantined: 0,
      remaining: 0,
      stoppedReason: "error",
    };
  }

  const isOffline = options?.isOfflineFn ?? isKioskOffline;
  if (isOffline()) {
    const remaining = await getPendingCount(kioskId);
    return {
      succeeded: 0,
      quarantined: 0,
      remaining,
      stoppedReason: "offline",
    };
  }

  const pendingItems = await getPendingQueueItems(kioskId);
  if (pendingItems.length === 0) {
    return {
      succeeded: 0,
      quarantined: 0,
      remaining: 0,
      stoppedReason: "empty",
    };
  }

  const leaseDurationMs = options?.leaseDurationMs ?? 15_000;
  const holderId =
    options?.holderId ??
    (typeof crypto !== "undefined" && crypto.randomUUID
      ? crypto.randomUUID()
      : `replay-holder-${Date.now()}-${Math.random()}`);

  const acquired = await acquireReplayLease(kioskId, holderId, leaseDurationMs);
  if (!acquired) {
    return {
      succeeded: 0,
      quarantined: 0,
      remaining: pendingItems.length,
      stoppedReason: "locked",
    };
  }

  let succeeded = 0;
  let quarantined = 0;
  let stoppedReason: ReplayResult["stoppedReason"] = undefined;

  const fetchFn = options?.fetchFn ?? resilientFetch;
  const config = getKioskConfig();

  try {
    for (const item of pendingItems) {
      // Replay must stop when connectivity drops again and resume in order afterwards
      if (isOffline()) {
        stoppedReason = "offline";
        break;
      }

      await renewReplayLease(kioskId, holderId, leaseDurationMs);

      const headers: Record<string, string> = {
        ...(item.request.headers ?? {}),
        "Idempotency-Key": item.idempotencyKey,
      };

      if (config?.token && !headers["Authorization"]) {
        headers["Authorization"] = `Bearer ${config.token}`;
      }

      try {
        const response = await fetchFn(item.request.url, {
          method: item.request.method,
          headers,
          body: item.request.body,
        });

        // 1. Success Outcome (2xx)
        if (response.ok) {
          let outcomeData: unknown = null;
          try {
            const text = await response.text();
            outcomeData = text ? JSON.parse(text) : { ok: true };
          } catch {
            outcomeData = { ok: true, status: response.status };
          }

          const updated = await updateQueueItemStatus(
            item.sequence,
            "done",
            undefined,
            outcomeData
          );
          succeeded += 1;
          options?.onItemReplayed?.(updated, "succeeded");
          continue;
        }

        // 2. Retryable 5xx or 408/429
        if (response.status >= 500 || response.status === 408 || response.status === 429) {
          await incrementAttemptCount(item.sequence);
          stoppedReason = isOffline() ? "offline" : "error";
          break;
        }

        // 3. Permanent Client Error (4xx other than 408/429) -> Quarantined immediately
        const problem = await parseProblem(response);
        const reason =
          problem.detail ||
          problem.title ||
          `HTTP ${response.status} rejected during replay`;

        const updated = await updateQueueItemStatus(
          item.sequence,
          "quarantined",
          reason,
          problem
        );
        quarantined += 1;
        options?.onItemReplayed?.(updated, "quarantined");
      } catch (err: any) {
        // Network failure, aborted timeout, or connectivity drop
        await incrementAttemptCount(item.sequence);
        stoppedReason = "offline";
        break;
      }
    }
  } finally {
    await releaseReplayLease(kioskId, holderId);
  }

  const remaining = await getPendingCount(kioskId);
  return {
    succeeded,
    quarantined,
    remaining,
    stoppedReason,
  };
}

let isReplayingTrigger = false;

export async function triggerReplay(options?: ReplayOptions): Promise<ReplayResult | null> {
  if (isReplayingTrigger) {
    return null;
  }
  isReplayingTrigger = true;
  try {
    return await replayQueue(options);
  } finally {
    isReplayingTrigger = false;
  }
}

export function initReplayEngine(options?: ReplayOptions): () => void {
  const unsubscribeReconnect = onKioskReconnect(() => {
    void triggerReplay(options);
  });

  // Replay on app start when queue is non-empty
  void triggerReplay(options);

  return () => {
    unsubscribeReconnect();
  };
}
