import { client } from "@hdms/api-client";
import { QueryClient } from "@tanstack/react-query";
import {
  clearKioskConfig,
  clearSessionId,
  getKioskConfig,
  getSessionId,
} from "./kiosk-config";
import { recordRequestFailure, recordRequestSuccess } from "./connectivity";

export const REQUEST_TIMEOUT_MS = 8000;
export const MAX_RETRY_COUNT = 3;

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

let activeSessionAbortController: AbortController | null = null;

export function getSessionAbortSignal(): AbortSignal {
  if (!activeSessionAbortController || activeSessionAbortController.signal.aborted) {
    activeSessionAbortController = new AbortController();
  }
  return activeSessionAbortController.signal;
}

export function abortActiveSessionRequests(): void {
  if (activeSessionAbortController && !activeSessionAbortController.signal.aborted) {
    activeSessionAbortController.abort("Session terminated or reset");
  }
  activeSessionAbortController = new AbortController();
}

export function isRetryableError(statusOrError: number | unknown): boolean {
  if (typeof statusOrError === "number") {
    // 5xx server errors, 408 Request Timeout, and 429 Rate Limited are retryable
    if (statusOrError >= 500 && statusOrError <= 599) return true;
    if (statusOrError === 408 || statusOrError === 429) return true;
    // Other 4xx client errors (400, 401, 403, 404, 409, 410, 422) are non-retryable
    if (statusOrError >= 400 && statusOrError < 500) return false;
    return false;
  }
  // Network failures, TypeErrors, timeouts, AbortError from timeout are retryable
  if (statusOrError instanceof Error) {
    if (statusOrError.name === "AbortError" && statusOrError.message?.includes("timeout")) {
      return true;
    }
    if (statusOrError.name === "TypeError") {
      return true;
    }
  }
  return true;
}

export function calculateRetryDelay(attemptIndex: number): number {
  // Exponential backoff + jitter: base 1000ms * 2^attempt, capped at 10000ms
  const base = Math.min(1000 * Math.pow(2, attemptIndex), 10000);
  const jitter = Math.random() * 200;
  return base + jitter;
}

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: (failureCount, error: any) => {
        if (failureCount >= MAX_RETRY_COUNT) return false;
        const status = error?.status ?? error?.statusCode;
        return isRetryableError(status ?? error);
      },
      retryDelay: (attemptIndex) => calculateRetryDelay(attemptIndex),
      refetchOnWindowFocus: false,
    },
    mutations: {
      retry: (failureCount, error: any) => {
        if (failureCount >= MAX_RETRY_COUNT) return false;
        const status = error?.status ?? error?.statusCode;
        return isRetryableError(status ?? error);
      },
      retryDelay: (attemptIndex) => calculateRetryDelay(attemptIndex),
    },
  },
});

export async function resilientFetch(
  input: RequestInfo | URL,
  init?: RequestInit
): Promise<Response> {
  const originalRequest = input instanceof Request ? input : new Request(input, init);

  // Preserve body and headers across retries
  let bodyBuffer: ArrayBuffer | null = null;
  if (originalRequest.body && !originalRequest.bodyUsed) {
    bodyBuffer = await originalRequest.clone().arrayBuffer();
  }

  let attempt = 0;

  while (true) {
    const timeoutController = new AbortController();
    const timeoutId = setTimeout(() => {
      timeoutController.abort("Request timeout (8s)");
    }, REQUEST_TIMEOUT_MS);

    // Merge abort signals if external signal was provided
    const externalSignal = init?.signal ?? originalRequest.signal;
    let abortListener: (() => void) | null = null;

    if (externalSignal) {
      if (externalSignal.aborted) {
        clearTimeout(timeoutId);
        throw externalSignal.reason ?? new DOMException("Aborted", "AbortError");
      }
      abortListener = () => {
        timeoutController.abort(externalSignal.reason);
      };
      externalSignal.addEventListener("abort", abortListener);
    }

    try {
      // Re-create request for retry if body was already consumed
      let reqToFetch: Request;
      if (attempt === 0) {
        reqToFetch = originalRequest;
      } else {
        const reqInit: RequestInit = {
          method: originalRequest.method,
          headers: originalRequest.headers,
          body: bodyBuffer,
          signal: timeoutController.signal,
          redirect: originalRequest.redirect,
        };
        reqToFetch = new Request(originalRequest.url, reqInit);
      }

      const response = await globalThis.fetch(reqToFetch, {
        signal: timeoutController.signal,
      });

      clearTimeout(timeoutId);
      if (abortListener && externalSignal) {
        externalSignal.removeEventListener("abort", abortListener);
      }

      // Check if status is 5xx or 408/429
      if (isRetryableError(response.status)) {
        if (attempt < MAX_RETRY_COUNT) {
          attempt += 1;
          const delay = calculateRetryDelay(attempt - 1);
          await new Promise((resolve) => setTimeout(resolve, delay));
          continue;
        }
        // Exhausted retries on 5xx
        recordRequestFailure();
        return response;
      }

      // Successful HTTP outcome (including normal business 4xx rejections)
      recordRequestSuccess();
      return response;
    } catch (err: any) {
      clearTimeout(timeoutId);
      if (abortListener && externalSignal) {
        externalSignal.removeEventListener("abort", abortListener);
      }

      // If externally aborted (e.g. session end), don't retry and rethrow immediately
      if (externalSignal?.aborted) {
        throw err;
      }

      if (isRetryableError(err) && attempt < MAX_RETRY_COUNT) {
        attempt += 1;
        const delay = calculateRetryDelay(attempt - 1);
        await new Promise((resolve) => setTimeout(resolve, delay));
        continue;
      }

      // Network request failure exhausted
      recordRequestFailure();
      throw err;
    }
  }
}

let isInitialized = false;

export function initKioskApi(baseUrl?: string): void {
  if (isInitialized) {
    return;
  }
  isInitialized = true;

  const resolvedBaseUrl = baseUrl ?? import.meta.env.VITE_API_BASE_URL ?? "/v1";
  client.setConfig({
    baseUrl: resolvedBaseUrl,
    fetch: resilientFetch,
  });

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

  client.interceptors.response.use(async (response) => {
    if (response.status === 401 || response.status === 403) {
      clearKioskConfig();
      clearSessionId();
    }
    return response;
  });
}

export function resetKioskApiForTesting(): void {
  isInitialized = false;
  scanSequence = 0;
  activeSessionAbortController = null;
}
