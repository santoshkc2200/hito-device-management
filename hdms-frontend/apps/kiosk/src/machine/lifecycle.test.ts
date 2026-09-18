import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import {
  buildKioskSessionMachine,
  assertMachineScreensMapped,
  logMachineStartup,
  executeResume,
  executeClose,
  executeCancel,
  sessionMachine,
} from "./session-machine";
import {
  getSessionId,
  setSessionId,
} from "../lib/kiosk-config";
import { createActor } from "xstate";
import * as apiClient from "@hdms/api-client";
import type { ScanResult, Session } from "@hdms/api-client";

describe("Kiosk Machine Lifecycle & Resilience", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    sessionStorage.clear();
    localStorage.clear();
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.useRealTimers();
    sessionStorage.clear();
  });

  it("resumesFromStoredSessionId on active session", async () => {
    const mockSession: Session = {
      id: "sess-active-123",
      kioskId: "kiosk-1",
      state: "ready",
      user: {
        id: "user-1",
        fullName: "Dr. Sharma",
        department: "Radiology",
        openLoanCount: 1,
      },
      startedAt: new Date().toISOString(),
      expiresAt: new Date(Date.now() + 20000).toISOString(),
    };

    vi.spyOn(apiClient, "getSession").mockResolvedValue({
      data: mockSession,
      response: new Response(),
    } as any);

    setSessionId("sess-active-123");
    expect(getSessionId()).toBe("sess-active-123");

    const resumed = await executeResume({ sessionId: "sess-active-123" });
    expect(resumed).toEqual(mockSession);
    expect(getSessionId()).toBe("sess-active-123");
  });

  it("startsCleanOnExpiredSession and clears storage", async () => {
    vi.spyOn(apiClient, "getSession").mockResolvedValue({
      data: {
        id: "sess-expired-123",
        kioskId: "kiosk-1",
        state: "expired",
        startedAt: new Date(Date.now() - 60000).toISOString(),
        expiresAt: new Date(Date.now() - 30000).toISOString(),
      },
      response: new Response(),
    } as any);

    setSessionId("sess-expired-123");
    const resumed = await executeResume({ sessionId: "sess-expired-123" });

    expect(resumed).toBeNull();
    expect(getSessionId()).toBeNull();
  });

  it("watchdogReturnsToIdleAfterThreeMinutes and clears user context", () => {
    const machine = buildKioskSessionMachine();
    const actor = createActor(machine);
    actor.start();

    // Restore to ready state with user and loan context
    actor.send({
      type: "RESTORE_SESSION",
      session: {
        id: "sess-watchdog-1",
        kioskId: "kiosk-1",
        state: "ready",
        user: {
          id: "user-1",
          fullName: "Dr. Sharma",
          department: "Radiology",
          openLoanCount: 1,
        },
        startedAt: new Date().toISOString(),
        expiresAt: new Date(Date.now() + 25000).toISOString(),
      },
      openLoans: [
        {
          id: "loan-1",
          deviceId: "dev-1",
          assetTag: "LAPTOP-07",
          deviceName: "Laptop",
          borrowedAt: new Date().toISOString(),
        },
      ],
    });

    expect(actor.getSnapshot().value).toBe("ready");
    expect(actor.getSnapshot().context.user?.fullName).toBe("Dr. Sharma");
    expect(actor.getSnapshot().context.openLoans.length).toBe(1);

    // Watchdog fires after 3 minutes
    actor.send({ type: "WATCHDOG_TIMEOUT" });

    const snapshot = actor.getSnapshot();
    expect(snapshot.value).toBe("idle");
    expect(snapshot.context.user).toBeNull();
    expect(snapshot.context.openLoans).toEqual([]);
    expect(snapshot.context.sessionId).toBeNull();

    actor.stop();
  });

  it("serverExpiresAtOverridesLocalTimer", () => {
    const machine = buildKioskSessionMachine();
    const actor = createActor(machine);
    actor.start();

    actor.send({
      type: "RESTORE_SESSION",
      session: {
        id: "sess-exp-1",
        kioskId: "kiosk-1",
        state: "awaiting_user",
        // Server expires in 5 seconds (shorter than local 45s timer)
        expiresAt: new Date(Date.now() + 5000).toISOString(),
        startedAt: new Date().toISOString(),
      },
    });

    expect(actor.getSnapshot().value).toBe("awaiting_user");

    // Advance 6 seconds so server expiresAt is in the past
    vi.advanceTimersByTime(6000);

    // Reconcile expiry (as triggered by visibility / timer / response)
    actor.send({ type: "RECONCILE_EXPIRY" });

    const snapshot = actor.getSnapshot();
    expect(snapshot.value).toBe("idle");
    expect(snapshot.context.sessionId).toBeNull();

    actor.stop();
  });

  it("duplicateOutcomeDoesNotResetCountdown", () => {
    const machine = buildKioskSessionMachine();
    const initialExpiresAt = new Date(Date.now() + 25000).toISOString();

    const actor = createActor(machine);
    actor.start();

    actor.send({
      type: "RESTORE_SESSION",
      session: {
        id: "sess-dup-1",
        kioskId: "kiosk-1",
        state: "ready",
        expiresAt: initialExpiresAt,
        startedAt: new Date().toISOString(),
      },
    });

    // Advance 10s
    vi.advanceTimersByTime(10000);

    // Receive a duplicate scan result
    const duplicateResult: ScanResult = {
      session: {
        id: "sess-dup-1",
        kioskId: "kiosk-1",
        state: "ready",
        startedAt: new Date().toISOString(),
        expiresAt: new Date(Date.now() + 25000).toISOString(), // server re-sent fresh
      },
      outcome: {
        kind: "duplicate",
      },
      openLoans: [],
      message: {
        title: "Already scanned",
        detail: "This device is already in session",
        tone: "info",
      },
    };

    actor.send({ type: "APPLY_SCAN_RESULT", result: duplicateResult });

    const snapshot = actor.getSnapshot();
    expect(snapshot.value).toBe("ready");
    // expiresAt was preserved from existing countdown, not overwritten
    expect(snapshot.context.expiresAt).toBe(initialExpiresAt);

    actor.stop();
  });

  it("problemResponseEntersFailureStateNotThrow", async () => {
    const machine = buildKioskSessionMachine();
    const actor = createActor(machine);
    actor.start();

    expect(() => {
      actor.send({
        type: "SET_PROBLEM",
        problem: {
          kind: "device-unavailable",
          type: "https://api.hito.internal/problems/device-unavailable",
          title: "Device Unavailable",
          status: 409,
          detail: "Device is undergoing maintenance",
          supportCode: "SUPP-TEST-123",
        },
      });
    }).not.toThrow();

    const snapshot = actor.getSnapshot();
    expect(snapshot.context.lastProblem?.kind).toBe("device-unavailable");
    expect(snapshot.context.supportCode).toBe("SUPP-TEST-123");

    actor.stop();
  });

  it("sessionIdIsClearedOnDone", async () => {
    vi.spyOn(apiClient, "closeSession").mockResolvedValue({
      data: undefined,
      response: new Response(),
    } as any);

    setSessionId("sess-done-123");
    expect(getSessionId()).toBe("sess-done-123");

    await executeClose({ sessionId: "sess-done-123" });
    expect(getSessionId()).toBeNull();
  });

  it("sessionIdIsClearedOnCancel", async () => {
    vi.spyOn(apiClient, "cancelSession").mockResolvedValue({
      data: undefined,
      response: new Response(),
    } as any);

    setSessionId("sess-cancel-123");
    expect(getSessionId()).toBe("sess-cancel-123");

    await executeCancel({ sessionId: "sess-cancel-123" });
    expect(getSessionId()).toBeNull();
  });

  it("assertMachineScreensMapped throws if a state is missing a screen", () => {
    const invalidDefinition: any = {
      version: "1.0.0",
      states: {
        idle: { on: {} },
        unmapped_mystery_state: { on: {} },
      },
    };

    expect(() => assertMachineScreensMapped(invalidDefinition)).toThrow(
      /Unmapped machine state: "unmapped_mystery_state"/
    );
  });

  it("logs machine version on startup", () => {
    const infoSpy = vi.spyOn(console, "info").mockImplementation(() => {});
    logMachineStartup();
    expect(infoSpy).toHaveBeenCalledWith(
      expect.stringContaining("[HDMS Kiosk] Machine definition version: 1.1.0")
    );
    infoSpy.mockRestore();
  });

  it("enforces machine purity with zero business-status enum branching", () => {
    const machineConfigStr = JSON.stringify(sessionMachine.config);

    // Machine implementation must not branch on specific business logic statuses:
    // "available", "maintenance", "on_loan", "suspended", "archived", etc.
    const forbiddenPatterns = [
      /status\s*===\s*["']available["']/,
      /status\s*===\s*["']on_loan["']/,
      /status\s*===\s*["']maintenance["']/,
      /status\s*===\s*["']suspended["']/,
      /status\s*===\s*["']archived["']/,
      /reason\s*===\s*["']lost["']/,
    ];

    for (const pattern of forbiddenPatterns) {
      expect(pattern.test(machineConfigStr)).toBe(false);
    }
  });
});
