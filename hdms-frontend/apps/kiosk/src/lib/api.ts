import { client } from "@hdms/api-client";
import { QueryClient } from "@tanstack/react-query";
import { getKioskConfig, getSessionId } from "./kiosk-config";

let scanSequence = 0;

export function getNextScanSequence(): number {
  scanSequence += 1;
  return scanSequence;
}

export function getCurrentScanSequence(): number {
  return scanSequence;
}

export function resetScanSequence(): void {
  scanSequence = 0;
}

export function deriveIdempotencyKey(
  kioskId: string,
  sessionId: string,
  sequence: number
): string {
  return `${kioskId}:${sessionId}:${sequence}`;
}

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
});

let isInitialized = false;

export function initKioskApi(baseUrl?: string): void {
  if (isInitialized) {
    return;
  }
  isInitialized = true;

  const resolvedBaseUrl = baseUrl ?? import.meta.env.VITE_API_BASE_URL ?? "/v1";
  client.setConfig({ baseUrl: resolvedBaseUrl });

  client.interceptors.request.use(async (request) => {
    const config = getKioskConfig();

    if (config?.token) {
      request.headers.set("Authorization", `Bearer ${config.token}`);
      request.headers.set("Cache-Control", "no-store");
    }

    if (request.method === "POST" && !request.headers.has("Idempotency-Key")) {
      const kioskId = config?.kioskId ?? "unpaired-kiosk";
      const sessionId = getSessionId() ?? "no-session";
      const seq = getCurrentScanSequence();
      request.headers.set("Idempotency-Key", deriveIdempotencyKey(kioskId, sessionId, seq));
    }

    return request;
  });
}
