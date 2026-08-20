import * as React from "react";
import { getHealthz } from "@hdms/api-client";

export interface ConnectivityState {
  isOffline: boolean;
  consecutiveFailures: number;
}

type ConnectivityListener = () => void;
type ReconnectCallback = () => void;

class ConnectivityManager {
  private isOffline = false;
  private consecutiveFailures = 0;
  private probeTimeoutId: ReturnType<typeof setTimeout> | null = null;
  private probeAttempt = 0;
  private listeners: Set<ConnectivityListener> = new Set();
  private reconnectCallbacks: Set<ReconnectCallback> = new Set();
  private probeFn: () => Promise<boolean>;

  constructor() {
    this.probeFn = async () => {
      try {
        const res = await getHealthz();
        return !res.error;
      } catch {
        return false;
      }
    };

    if (typeof window !== "undefined") {
      window.addEventListener("online", () => this.handleOnlineEvent());
      window.addEventListener("offline", () => this.handleOfflineEvent());
    }
  }

  public setProbeFn(fn: () => Promise<boolean>): void {
    this.probeFn = fn;
  }

  public getState(): ConnectivityState {
    return {
      isOffline: this.isOffline,
      consecutiveFailures: this.consecutiveFailures,
    };
  }

  public getIsOffline(): boolean {
    return this.isOffline;
  }

  public subscribe(listener: ConnectivityListener): () => void {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  }

  public onReconnect(cb: ReconnectCallback): () => void {
    this.reconnectCallbacks.add(cb);
    return () => {
      this.reconnectCallbacks.delete(cb);
    };
  }

  private notify(): void {
    for (const listener of this.listeners) {
      listener();
    }
  }

  private notifyReconnect(): void {
    for (const cb of this.reconnectCallbacks) {
      try {
        cb();
      } catch (err) {
        console.error("[Connectivity] Error in reconnect callback", err);
      }
    }
  }

  public recordRequestSuccess(): void {
    const wasOffline = this.isOffline;
    this.consecutiveFailures = 0;
    this.stopBackgroundProbe();

    if (wasOffline) {
      this.isOffline = false;
      this.notify();
      this.notifyReconnect();
    }
  }

  public recordRequestFailure(): void {
    this.consecutiveFailures += 1;

    if (this.consecutiveFailures >= 2 && !this.isOffline) {
      this.isOffline = true;
      this.notify();
      this.startBackgroundProbe();
    }
  }

  public handleOnlineEvent(): void {
    // navigator.online event is only a hint to probe sooner, not proof of gateway connectivity
    if (this.isOffline) {
      this.probeAttempt = 0;
      this.scheduleProbe(0);
    }
  }

  public handleOfflineEvent(): void {
    // Treat explicit offline event as failure hint
    if (!this.isOffline) {
      this.recordRequestFailure();
    }
  }

  public startBackgroundProbe(): void {
    this.stopBackgroundProbe();
    this.probeAttempt = 0;
    this.scheduleProbe(1000);
  }

  public stopBackgroundProbe(): void {
    if (this.probeTimeoutId !== null) {
      clearTimeout(this.probeTimeoutId);
      this.probeTimeoutId = null;
    }
    this.probeAttempt = 0;
  }

  private scheduleProbe(delayMs: number): void {
    if (this.probeTimeoutId !== null) {
      clearTimeout(this.probeTimeoutId);
    }

    this.probeTimeoutId = setTimeout(async () => {
      this.probeTimeoutId = null;
      if (!this.isOffline) return;

      const success = await this.probeFn();
      if (success) {
        this.recordRequestSuccess();
      } else {
        this.probeAttempt += 1;
        // Exponential backoff capped at 10 seconds: 1s, 2s, 4s, 8s, 10s...
        const nextDelay = Math.min(1000 * Math.pow(2, this.probeAttempt), 10000);
        this.scheduleProbe(nextDelay);
      }
    }, delayMs);
  }

  public resetForTesting(initialOffline = false): void {
    this.stopBackgroundProbe();
    this.isOffline = initialOffline;
    this.consecutiveFailures = 0;
    this.probeAttempt = 0;
    this.notify();
  }
}

export const connectivityManager = new ConnectivityManager();

export function isKioskOffline(): boolean {
  return connectivityManager.getIsOffline();
}

export function subscribeConnectivity(listener: ConnectivityListener): () => void {
  return connectivityManager.subscribe(listener);
}

export function onKioskReconnect(cb: ReconnectCallback): () => void {
  return connectivityManager.onReconnect(cb);
}

export function recordRequestSuccess(): void {
  connectivityManager.recordRequestSuccess();
}

export function recordRequestFailure(): void {
  connectivityManager.recordRequestFailure();
}

export function resetConnectivityForTesting(initialOffline = false): void {
  connectivityManager.resetForTesting(initialOffline);
}

export function useConnectivity(): {
  isOffline: boolean;
  consecutiveFailures: number;
} {
  const isOffline = React.useSyncExternalStore(
    subscribeConnectivity,
    () => connectivityManager.getIsOffline(),
    () => false
  );

  return {
    isOffline,
    consecutiveFailures: connectivityManager.getState().consecutiveFailures,
  };
}
