import { act, renderHook, waitFor } from "@testing-library/react";
import { createActor } from "xstate";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ScanResult } from "@hdms/api-client";
import { sessionMachine, executeScan, executeResume, executeClose, executeReturnLoan, type ScanActorInput } from "./session-machine";
import { useKioskSession } from "./use-kiosk-session";
import { setKioskConfig } from "../lib/kiosk-config";

vi.mock("./session-machine", async (original: <T>() => Promise<T>) => ({
  ...await original<typeof import("./session-machine")>(),
  executeScan: vi.fn(),
  executeClose: vi.fn().mockResolvedValue(undefined),
  executeResume: vi.fn().mockResolvedValue(null),
  executeReturnLoan: vi.fn(),
}));

const user = { id: "user-1", fullName: "First User", department: "Ward", openLoanCount: 0 };
const devices = [1, 2, 3].map((n) => ({ id: `device-${n}`, name: `Item ${n}`, assetTag: `ASSET-${n}` }));
const actors: ReturnType<typeof createActor<typeof sessionMachine>>[] = [];

function result(kind: ScanResult["outcome"]["kind"], deviceIndex?: number, identified = true): ScanResult {
  return {
    session: {
      id: "session-1", kioskId: "kiosk-1", state: identified ? "ready" : "awaiting_user",
      ...(identified ? { user } : { pendingDevice: devices[deviceIndex ?? 0] }),
      startedAt: new Date().toISOString(), expiresAt: new Date(Date.now() + 60000).toISOString(),
    },
    outcome: { kind, ...(deviceIndex === undefined ? {} : { device: devices[deviceIndex] }) },
    openLoans: [],
    message: { title: "Outcome", detail: "", tone: kind === "rejected" ? "warning" : "success", key: kind === "rejected" ? "device_reserved" : kind },
  };
}

function setup() {
  const actor = createActor(sessionMachine).start();
  actors.push(actor);
  return renderHook(() => useKioskSession({ actor }));
}

beforeEach(() => {
  localStorage.clear();
  sessionStorage.clear();
  vi.resetAllMocks();
  vi.mocked(executeResume).mockResolvedValue(null);
  vi.mocked(executeClose).mockResolvedValue(undefined);
  setKioskConfig({ kioskId: "kiosk-1", kioskName: "Ward", token: "token", enabledSources: ["manual"] });
});
afterEach(() => { actors.splice(0).forEach((actor) => actor.stop()); });

describe("kiosk transaction session", () => {
  it("retains successes and failures in scan order", async () => {
    vi.mocked(executeScan).mockResolvedValueOnce(result("user_identified"))
      .mockResolvedValueOnce(result("borrowed", 0))
      .mockResolvedValueOnce(result("rejected", 1))
      .mockResolvedValueOnce(result("returned", 2));
    const { result: hook } = setup();
    await act(async () => { await hook.current.startScanning(); });
    for (const token of ["card", "device-1", "device-2", "device-3"]) {
      await act(async () => { await hook.current.scan(token); });
    }
    expect(hook.current.transactions?.map((row) => [row.device?.name, row.status])).toEqual([
      ["Item 1", "borrowed"], ["Item 2", "failed"], ["Item 3", "returned"],
    ]);
  });

  it("resolves every device scanned before the user card, including the refused pending device", async () => {
    vi.mocked(executeScan).mockResolvedValueOnce(result("device_pending", 0, false))
      .mockResolvedValueOnce(result("device_pending", 1, false))
      .mockResolvedValueOnce(result("rejected", 1))
      .mockResolvedValueOnce(result("borrowed", 0));
    const { result: hook } = setup();
    await act(async () => { await hook.current.startScanning(); });
    for (const token of ["device-1", "device-2", "card"]) {
      await act(async () => { await hook.current.scan(token); });
    }
    expect(hook.current.transactions?.map((row) => [row.device?.id, row.status])).toEqual([
      ["device-1", "borrowed"], ["device-2", "failed"],
    ]);
  });

  it("serializes rapid scans into one session and Finish waits for accepted scans", async () => {
    let release!: (r: ScanResult) => void;
    vi.mocked(executeScan).mockImplementationOnce(() => new Promise((resolve) => { release = resolve; }))
      .mockImplementationOnce(async (input: ScanActorInput) => {
        if (input.sessionId !== "session-1") throw new Error("scan lost session identity");
        return result("borrowed", 0);
      });
    const { result: hook } = setup();
    await act(async () => { await hook.current.startScanning(); });
    let card!: Promise<void>, device!: Promise<void>, finish!: Promise<void>;
    await act(async () => {
      card = hook.current.scan("card");
      device = hook.current.scan("device-1");
      expect(hook.current.finish).toBeTypeOf("function");
      finish = hook.current.finish();
    });
    expect(hook.current.isFinished).toBe(false);
    await act(async () => { release(result("user_identified")); await Promise.all([card, device, finish]); });
    expect(hook.current.isFinished).toBe(true);
    expect(hook.current.transactions?.map((row) => row.status)).toEqual(["borrowed"]);
    await act(async () => { await hook.current.scan("device-2"); });
    expect(hook.current.transactions).toHaveLength(1);
    await act(async () => { await hook.current.close(); });
    expect(hook.current.isFinished).toBe(false);
    expect(hook.current.transactions).toEqual([]);
    expect(hook.current.state).toBe("idle");
  });

  it("keeps request failures in the final summary", async () => {
    vi.mocked(executeScan).mockRejectedValueOnce({ kind: "device-unavailable", type: "https://hdms/problems/device-unavailable", title: "Unavailable", status: 409, supportCode: "TEST" });
    const { result: hook } = setup();
    await act(async () => { await hook.current.startScanning(); await hook.current.scan("device-1"); });
    expect(hook.current.transactions?.map((row) => row.status)).toEqual(["failed"]);
  });

  it("discards saved attempts when the stored session has expired", async () => {
    vi.mocked(executeScan).mockResolvedValueOnce(result("device_pending", 0, false));
    const first = setup();
    await act(async () => { await first.result.current.startScanning(); await first.result.current.scan("device-1"); });
    first.unmount();
    const resumed = setup();
    await waitFor(() => expect(resumed.result.current.transactions).toEqual([]));
  });

  it("does not toggle a saved pending device after the server has already identified the user", async () => {
    vi.mocked(executeScan).mockResolvedValueOnce(result("device_pending", 0, false));
    const first = setup();
    await act(async () => { await first.result.current.startScanning(); await first.result.current.scan("device-1"); });
    first.unmount();
    vi.mocked(executeResume).mockResolvedValueOnce(result("borrowed", 0).session);
    vi.mocked(executeScan).mockResolvedValueOnce(result("borrowed", 1)).mockResolvedValueOnce(result("returned", 0));
    const resumed = setup();
    await waitFor(() => expect(resumed.result.current.context.user?.id).toBe("user-1"));
    await act(async () => { await resumed.result.current.scan("device-2"); });
    expect(resumed.result.current.transactions.map((row) => [row.device?.id, row.status, row.failureReason])).toEqual([
      ["device-1", "failed", "result-unconfirmed"], ["device-2", "borrowed", undefined],
    ]);
  });

  it("keeps a later device distinct from the pending device when a card response was lost", async () => {
    vi.mocked(executeScan).mockResolvedValueOnce(result("device_pending", 0, false))
      .mockRejectedValueOnce(new TypeError("Network response lost"))
      .mockResolvedValueOnce(result("borrowed", 2))
      .mockResolvedValueOnce(result("returned", 0));
    const { result: hook } = setup();
    await act(async () => { await hook.current.startScanning(); });
    for (const token of ["device-1", "card", "device-3"]) {
      await act(async () => { await hook.current.scan(token); });
    }
    expect(hook.current.transactions.map((row) => [row.device?.id, row.status])).toEqual([
      ["device-1", "failed"], [undefined, "failed"], ["device-3", "borrowed"],
    ]);
  });

  it("keeps accepted attempts in the summary if the session expires while Finish waits", async () => {
    let rejectRequest!: (error: Error) => void;
    vi.mocked(executeScan).mockResolvedValueOnce(result("user_identified"))
      .mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectRequest = reject; }))
      .mockResolvedValueOnce(result("borrowed", 1));
    const { result: hook } = setup();
    await act(async () => { await hook.current.startScanning(); await hook.current.scan("card"); });
    let first!: Promise<void>, second!: Promise<void>, finish!: Promise<void>;
    await act(async () => {
      first = hook.current.scan("device-1"); second = hook.current.scan("device-2"); finish = hook.current.finish();
    });
    await act(async () => {
      actors.at(-1)!.send({ type: "TIMEOUT" });
      rejectRequest(new DOMException("Aborted", "AbortError"));
      await Promise.all([first, second, finish]);
    });
    expect(hook.current.isFinished).toBe(true);
    expect(hook.current.transactions.map((row) => [row.status, row.failureReason])).toEqual([
      ["failed", "result-unconfirmed"], ["failed", "session-expired"],
    ]);
    expect(hook.current.state).toBe("idle");
  });

  it("retains the identities of queued returns when expiry interrupts Finish", async () => {
    let rejectRequest!: (error: Error) => void;
    const identified = result("user_identified");
    identified.openLoans = devices.slice(0, 2).map((device, index) => ({ id: `loan-${index + 1}`, deviceId: device.id, deviceName: device.name, assetTag: device.assetTag, borrowedAt: new Date().toISOString() }));
    vi.mocked(executeScan).mockResolvedValueOnce(identified);
    vi.mocked(executeReturnLoan).mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectRequest = reject; }));
    const { result: hook } = setup();
    await act(async () => { await hook.current.startScanning(); await hook.current.scan("card"); });
    let first!: Promise<void>, second!: Promise<void>, finish!: Promise<void>;
    await act(async () => {
      first = hook.current.returnLoan("loan-1"); second = hook.current.returnLoan("loan-2"); finish = hook.current.finish();
    });
    await act(async () => {
      actors.at(-1)!.send({ type: "TIMEOUT" });
      rejectRequest(new DOMException("Aborted", "AbortError"));
      await Promise.all([first, second, finish]);
    });
    expect(hook.current.transactions.map((row) => [row.device?.id, row.status, row.failureReason])).toEqual([
      ["device-1", "failed", "result-unconfirmed"], ["device-2", "failed", "session-expired"],
    ]);
  });
});
