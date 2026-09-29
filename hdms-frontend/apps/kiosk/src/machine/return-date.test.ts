import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createActor } from "xstate";
import * as apiClient from "@hdms/api-client";
import type { ScanResult } from "@hdms/api-client";
import { buildKioskSessionMachine, executeScan, executeSetDueDate } from "./session-machine";

const borrowResult = (dueAt: string): ScanResult => ({
  session: {
    id: "sess-1",
    kioskId: "kiosk-1",
    state: "ready",
    user: { id: "u1", fullName: "Dr. Sharma", department: "Radiology", openLoanCount: 1 },
    startedAt: new Date().toISOString(),
    expiresAt: new Date(Date.now() + 25_000).toISOString(),
  },
  outcome: { kind: "borrowed", loanId: "loan-1", dueAt, latestReturnAt: "2026-10-01T08:00:00Z" },
  openLoans: [{ id: "loan-1", deviceId: "d1", assetTag: "A1", deviceName: "iPad", borrowedAt: new Date().toISOString(), dueAt }],
  message: { title: "", detail: "", tone: "success" },
});

describe("return date in the kiosk session", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    sessionStorage.clear();
    vi.clearAllMocks();
  });
  afterEach(() => vi.useRealTimers());

  it("LOAN_DUE_UPDATED updates the outcome, the open loan and the preferred date, and restarts ready's timer", () => {
    const actor = createActor(buildKioskSessionMachine()).start();
    actor.send({ type: "APPLY_SCAN_RESULT", result: borrowResult("2026-09-30T08:00:00Z") });
    expect(actor.getSnapshot().value).toBe("ready");

    vi.advanceTimersByTime(20_000);
    actor.send({
      type: "LOAN_DUE_UPDATED",
      loanId: "loan-1",
      dueAt: "2026-09-30T06:00:00Z",
      latestReturnAt: "2026-10-01T08:00:00Z",
      sessionExpiresAt: new Date(Date.now() + 25_000).toISOString(),
    });
    const ctx = actor.getSnapshot().context;
    expect(ctx.lastOutcome?.dueAt).toBe("2026-09-30T06:00:00Z");
    expect(ctx.openLoans[0].dueAt).toBe("2026-09-30T06:00:00Z");
    expect(ctx.preferredDueAt).toBe("2026-09-30T06:00:00Z");

    vi.advanceTimersByTime(20_000);
    expect(actor.getSnapshot().value).toBe("ready");
    vi.advanceTimersByTime(6_000);
    expect(actor.getSnapshot().value).toBe("idle");
    expect(actor.getSnapshot().context.preferredDueAt).toBeNull();
  });

  it("executeScan sends preferredDueAt", async () => {
    const spy = vi.spyOn(apiClient, "submitScan").mockResolvedValue({
      data: borrowResult("2026-09-30T08:00:00Z"),
      response: new Response(),
    } as any);
    await executeScan({ sessionId: "sess-1", kioskId: "kiosk-1", token: "t", source: "scanner", preferredDueAt: "2026-09-30T06:00:00Z" });
    expect(spy.mock.calls[0][0].body).toMatchObject({ preferredDueAt: "2026-09-30T06:00:00Z" });
  });

  it("executeSetDueDate maps a due-date-conflict problem", async () => {
    vi.spyOn(apiClient, "setSessionLoanDueDate").mockResolvedValue({
      data: undefined,
      error: { type: "https://hdms.example/problems/due-date-conflict", status: 409, latestReturnAt: "2026-09-30T04:00:00Z" },
      response: new Response(null, { status: 409 }),
    } as any);
    await expect(
      executeSetDueDate({ sessionId: "sess-1", loanId: "loan-1", dueAt: "2026-10-02T08:00:00Z" })
    ).resolves.toEqual({ ok: false, conflict: true, latestReturnAt: "2026-09-30T04:00:00Z" });
  });

  it("executeSetDueDate maps success", async () => {
    vi.spyOn(apiClient, "setSessionLoanDueDate").mockResolvedValue({
      data: { loanId: "loan-1", dueAt: "2026-09-30T06:00:00Z", sessionExpiresAt: "2026-09-29T10:00:25Z" },
      response: new Response(),
    } as any);
    await expect(
      executeSetDueDate({ sessionId: "sess-1", loanId: "loan-1", dueAt: "2026-09-30T06:00:00Z" })
    ).resolves.toEqual({ ok: true, loanId: "loan-1", dueAt: "2026-09-30T06:00:00Z", latestReturnAt: null, sessionExpiresAt: "2026-09-29T10:00:25Z" });
  });
});
