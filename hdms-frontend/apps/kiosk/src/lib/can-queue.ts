import { isLocale, translate, type Locale } from "@hdms/i18n";
import { catalogue } from "../i18n";
import { CACHE_STALENESS_BOUND_MS, type CacheEntry, type CachedDevice } from "./offline-cache";
import { getKioskConfig } from "./kiosk-config";

export type QueueableAction = "borrow" | "return";

export interface QueueDecision {
  allowed: true;
  canQueue: true;
  action: QueueableAction;
}

export type RefusalReasonCode =
  | "no_cache"
  | "stale_cache"
  | "device_unavailable"
  | "unsupported_action";

export interface RefusalDecision {
  allowed: false;
  canQueue: false;
  action?: string;
  reason: string;
  reasonCode: RefusalReasonCode;
  refusalKey: string;
}

export type CanQueueResult = QueueDecision | RefusalDecision;

export interface CanQueueOptions {
  now?: number;
  stalenessBoundMs?: number;
  locale?: Locale;
}

export type CachedDeviceState =
  | CacheEntry<CachedDevice>
  | {
      status?: string;
      isFresh?: boolean;
      cachedAt?: number;
      ageMs?: number;
      data?: {
        status?: string;
        [key: string]: unknown;
      };
      [key: string]: unknown;
    }
  | null
  | undefined;

function getEffectiveLocale(explicitLocale?: Locale): Locale {
  if (explicitLocale && isLocale(explicitLocale)) {
    return explicitLocale;
  }
  const config = getKioskConfig();
  if (config?.defaultLocale && isLocale(config.defaultLocale)) {
    return config.defaultLocale;
  }
  return "en";
}

function createRefusal(
  reasonCode: RefusalReasonCode,
  refusalKey:
    | "offline.borrowRefusedStale"
    | "offline.borrowRefusedUnavailable"
    | "offline.borrowRefusedNoCache"
    | "offline.unsupportedAction",
  locale?: Locale,
  action?: string
): RefusalDecision {
  const effectiveLocale = getEffectiveLocale(locale);
  const reason = translate(catalogue, effectiveLocale, refusalKey);

  return {
    allowed: false,
    canQueue: false,
    action,
    reason,
    reasonCode,
    refusalKey,
  };
}

export function canQueue(
  action: string,
  cachedState?: CachedDeviceState,
  options?: CanQueueOptions
): CanQueueResult {
  const normalizedAction = action?.toLowerCase().trim();

  // A RETURN is always queueable, even with no cache at all.
  if (normalizedAction === "return") {
    return {
      allowed: true,
      canQueue: true,
      action: "return",
    };
  }

  // Only borrow and return can be processed through the offline queue.
  if (normalizedAction !== "borrow") {
    return createRefusal(
      "unsupported_action",
      "offline.unsupportedAction",
      options?.locale,
      action
    );
  }

  // A BORROW is queueable only if the cached device state is fresh AND says available.
  if (!cachedState) {
    return createRefusal(
      "no_cache",
      "offline.borrowRefusedNoCache",
      options?.locale,
      action
    );
  }

  // Determine freshness
  const stateRecord = cachedState as Record<string, unknown>;
  let isFresh: boolean;
  if (typeof stateRecord.isFresh === "boolean") {
    isFresh = stateRecord.isFresh;
  } else if (typeof stateRecord.cachedAt === "number") {
    const now = options?.now ?? Date.now();
    const bound = options?.stalenessBoundMs ?? CACHE_STALENESS_BOUND_MS;
    isFresh = now - stateRecord.cachedAt <= bound;
  } else if (typeof stateRecord.ageMs === "number") {
    const bound = options?.stalenessBoundMs ?? CACHE_STALENESS_BOUND_MS;
    isFresh = stateRecord.ageMs <= bound;
  } else {
    // Fail closed: a cached state whose age cannot be established is not
    // evidence of current custody, and a borrow may only ride on evidence.
    isFresh = false;
  }

  if (!isFresh) {
    return createRefusal(
      "stale_cache",
      "offline.borrowRefusedStale",
      options?.locale,
      action
    );
  }

  // Determine device status
  const rawStatus =
    (typeof stateRecord.status === "string" ? stateRecord.status : undefined) ??
    cachedState.data?.status;
  const normalizedStatus = rawStatus ? String(rawStatus).toLowerCase() : "";

  if (normalizedStatus !== "available") {
    return createRefusal(
      "device_unavailable",
      "offline.borrowRefusedUnavailable",
      options?.locale,
      action
    );
  }

  return {
    allowed: true,
    canQueue: true,
    action: "borrow",
  };
}
