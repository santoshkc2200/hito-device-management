import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
import {
  buildKioskSessionMachine,
  executeResume,
  setupVisibilityReconciliation,
  executeScan,
} from "./session-machine";
import {
  setSessionId,
  getSessionId,
} from "../lib/kiosk-config";
import {
  resetConnectivityForTesting,
} from "../lib/connectivity";
import {
  getSessionAbortSignal,
  resetScanSequence,
} from "../lib/api";
import { createActor } from "xstate";
import * as apiClient from "@hdms/api-client";
import type { Session } from "@hdms/api-client";
import { CountdownRing } from "../components/countdown-timer";

describe("Phase 3.8 Machine Resilience & Resync", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    sessionStorage.clear();
    localStorage.clear();
    resetScanSequence();
    resetConnectivityForTesting(false);
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.useRealTimers();
    sessionStorage.clear();
  });

  it("countdownSuspendedWhileOffline — freezes remaining time when isSuspended is true", () => {
    // 8-second expiry
    const futureExpiry = new Date(Date.now() + 8000).toISOString();

    const { rerender } = render(
      <CountdownRing expiresAt={futureExpiry} isSuspended={false} showAlways />
    );

    expect(screen.getByTestId("countdown-ring")).toHaveAttribute(
      "data-seconds-remaining",
      "8"
    );

    // Advance 3 seconds while online
    vi.advanceTimersByTime(3000);
    rerender(<CountdownRing expiresAt={futureExpiry} isSuspended={false} showAlways />);
    expect(screen.getByTestId("countdown-ring")).toHaveAttribute(
      "data-seconds-remaining",
      "5"
    );

    // Kiosk goes offline -> isSuspended = true
    rerender(<CountdownRing expiresAt={futureExpiry} isSuspended={true} showAlways />);

    // Advance 4 more seconds while offline
    vi.advanceTimersByTime(4000);
    rerender(<CountdownRing expiresAt={futureExpiry} isSuspended={true} showAlways />);

    // Countdown MUST remain suspended at 5s, NOT tick down to 1s or expire
    expect(screen.getByTestId("countdown-ring")).toHaveAttribute(
      "data-seconds-remaining",
      "5"
    );
  });

  it("resyncAfterReconnectRestoresCorrectScreen on alive session", async () => {
    const machine = buildKioskSessionMachine();
    const actor = createActor(machine);
    actor.start();

    const mockServerSession: Session = {
      id: "sess-reconnect-123",
      kioskId: "kiosk-1",
      state: "awaiting_device",
      user: {
        id: "user-42",
        fullName: "Dr. A. Sharma",
        department: "Radiology",
        openLoanCount: 1,
      },
      startedAt: new Date().toISOString(),
      expiresAt: new Date(Date.now() + 35000).toISOString(),
    };

    vi.spyOn(apiClient, "getSession").mockResolvedValue({
      data: mockServerSession,
      response: new Response(),
    } as any);

    setSessionId("sess-reconnect-123");

    // Reconnect fires
    const res = await executeResume({ sessionId: "sess-reconnect-123" });
    expect(res).toEqual(mockServerSession);

    actor.send({ type: "RESTORE_SESSION", session: res! });

    const snapshot = actor.getSnapshot();
    expect(snapshot.value).toBe("awaiting_device");
    expect(snapshot.context.user?.fullName).toBe("Dr. A. Sharma");
    expect(snapshot.context.sessionId).toBe("sess-reconnect-123");

    actor.stop();
  });

  it("resyncAfterReconnectCleansToIdleOnExpiredSession", async () => {
    const machine = buildKioskSessionMachine();
    const actor = createActor(machine);
    actor.start();

    // Server says session expired while offline
    vi.spyOn(apiClient, "getSession").mockResolvedValue({
      data: {
        id: "sess-expired-999",
        kioskId: "kiosk-1",
        state: "expired",
        startedAt: new Date(Date.now() - 60000).toISOString(),
        expiresAt: new Date(Date.now() - 10000).toISOString(),
      },
      response: new Response(),
    } as any);

    setSessionId("sess-expired-999");

    const res = await executeResume({ sessionId: "sess-expired-999" });
    expect(res).toBeNull();
    expect(getSessionId()).toBeNull();

    // Actor returns to idle
    actor.send({ type: "RESET" });
    expect(actor.getSnapshot().value).toBe("idle");
    expect(actor.getSnapshot().context.sessionId).toBeNull();

    actor.stop();
  });

  it("abortedRequestCannotRepaintANewSession", async () => {
    const machine = buildKioskSessionMachine();
    const actor = createActor(machine);
    actor.start();

    // Step 1: Start a scan that takes 5 seconds
    vi.spyOn(apiClient, "submitScan").mockImplementation(
      (({ signal }: { signal?: AbortSignal }) =>
        new Promise((_, reject) => {
          if (signal) {
            signal.addEventListener("abort", () => {
              reject(new DOMException("Aborted", "AbortError"));
            });
          }
        })) as any
    );

    setSessionId("sess-old");

    const scanPromise = executeScan({
      sessionId: "sess-old",
      kioskId: "kiosk-1",
      token: "HD-U-OLD-USER",
      source: "scanner",
    });

    // Step 2: User walks away / timeout / watchdog aborts the session
    actor.send({ type: "WATCHDOG_TIMEOUT" });
    expect(actor.getSnapshot().value).toBe("idle");
    expect(actor.getSnapshot().context.sessionId).toBeNull();

    // The signal was aborted
    const signal = getSessionAbortSignal();
    expect(signal.aborted).toBe(false); // reset controller created fresh for next session

    // Step 3: The slow request completes or is rejected
    await expect(scanPromise).rejects.toThrow(/Aborted|AbortError/);

    // Ensure screen context remains idle and was NOT overwritten by late response
    expect(actor.getSnapshot().value).toBe("idle");
    expect(actor.getSnapshot().context.user).toBeNull();

    actor.stop();
  });

  it("visibilityChangeReconcilesServerSessionWhenHiddenMoreThan30Seconds", async () => {
    const machine = buildKioskSessionMachine();
    const actor = createActor(machine);
    actor.start();

    const mockServerSession: Session = {
      id: "sess-vis-1",
      kioskId: "kiosk-1",
      state: "ready",
      user: {
        id: "user-1",
        fullName: "Dr. Sharma",
        department: "Radiology",
        openLoanCount: 0,
      },
      startedAt: new Date().toISOString(),
      expiresAt: new Date(Date.now() + 20000).toISOString(),
    };

    const getSessionSpy = vi.spyOn(apiClient, "getSession").mockResolvedValue({
      data: mockServerSession,
      response: new Response(),
    } as any);

    setSessionId("sess-vis-1");

    const cleanup = setupVisibilityReconciliation(actor);

    // Simulate iPad screen locking / going hidden
    Object.defineProperty(document, "visibilityState", {
      value: "hidden",
      writable: true,
    });
    document.dispatchEvent(new Event("visibilitychange"));

    // Advance 45 seconds (exceeding the 30-second threshold)
    vi.advanceTimersByTime(45000);

    // Simulate unlocking / returning to foreground
    Object.defineProperty(document, "visibilityState", {
      value: "visible",
      writable: true,
    });
    document.dispatchEvent(new Event("visibilitychange"));

    // Verify executeResume was called to reconcile stale state
    await vi.advanceTimersByTimeAsync(50);
    expect(getSessionSpy).toHaveBeenCalledWith({ path: { id: "sess-vis-1" } });

    cleanup();
    actor.stop();
  });
});
