import * as React from "react";
import { client, getReadyz } from "@hdms/api-client";

// While a restore runs, the API answers 503 with problem type "maintenance"
// (hdms-backend/internal/apiserver/maintenance.go). Every app shows a notice
// instead of its usual error or offline screen and asks /v1/readyz every 15
// seconds; a 200 there means the restore is over. One store for all apps, so
// the kiosk, staff and admin cannot disagree about what "over" means.
export const MAINTENANCE_RECHECK_MS = 15_000;

type Listener = () => void;

const listeners = new Set<Listener>();
let active = false;
let timer: ReturnType<typeof setTimeout> | null = null;

async function readyzIsReady(): Promise<boolean> {
  const { response } = await getReadyz();
  return response?.ok ?? false;
}

let probe: () => Promise<boolean> = readyzIsReady;

function notify(): void {
  for (const listener of listeners) listener();
}

// Anything but a 200 — "not-ready" while the swap terminates connections, a
// network error, the maintenance answer itself — keeps the notice up.
function scheduleRecheck(): void {
  if (timer !== null) return;
  timer = setTimeout(async () => {
    timer = null;
    if (!active) return;
    let ready = false;
    try {
      ready = await probe();
    } catch {
      ready = false;
    }
    if (ready) clearMaintenance();
    else scheduleRecheck();
  }, MAINTENANCE_RECHECK_MS);
}

export function reportMaintenance(): void {
  const wasActive = active;
  active = true;
  scheduleRecheck();
  if (!wasActive) notify();
}

export function clearMaintenance(): void {
  if (timer !== null) {
    clearTimeout(timer);
    timer = null;
  }
  if (!active) return;
  active = false;
  notify();
}

export function isUnderMaintenance(): boolean {
  return active;
}

export function subscribeMaintenance(listener: Listener): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** Resolves once maintenance is over — at once if it is not on. */
export function maintenanceEnded(): Promise<void> {
  if (!active) return Promise.resolve();
  return new Promise((resolve) => {
    const unsubscribe = subscribeMaintenance(() => {
      if (active) return;
      unsubscribe();
      resolve();
    });
  });
}

/** True while maintenance is on. `onEnd` runs once each time it ends — apps refetch there. */
export function useMaintenance(onEnd?: () => void): boolean {
  const on = React.useSyncExternalStore(subscribeMaintenance, isUnderMaintenance, () => false);
  const onEndRef = React.useRef(onEnd);
  React.useLayoutEffect(() => {
    onEndRef.current = onEnd;
  });
  const wasOn = React.useRef(on);
  React.useEffect(() => {
    if (wasOn.current && !on) onEndRef.current?.();
    wasOn.current = on;
  }, [on]);
  return on;
}

/** A 503 whose problem type is maintenance. Reads a clone; the body stays unread. */
export async function isMaintenanceResponse(response: Response): Promise<boolean> {
  if (response.status !== 503) return false;
  try {
    const body = (await response.clone().json()) as { type?: unknown };
    return typeof body.type === "string" && body.type.endsWith("/maintenance");
  } catch {
    return false;
  }
}

let interceptorInstalled = false;

/** For apps that call the API through the generated client (admin, staff). */
export function installMaintenanceInterceptor(): void {
  if (interceptorInstalled) return;
  interceptorInstalled = true;
  client.interceptors.response.use(async (response) => {
    if (await isMaintenanceResponse(response)) reportMaintenance();
    return response;
  });
}

export function resetMaintenanceForTesting(nextProbe: () => Promise<boolean> = readyzIsReady): void {
  if (timer !== null) clearTimeout(timer);
  timer = null;
  active = false;
  probe = nextProbe;
  notify();
}
