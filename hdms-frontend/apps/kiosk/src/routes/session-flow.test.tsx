import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { axe } from "vitest-axe";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ScanEvent } from "@hdms/scan";
import type { ScanResult } from "@hdms/api-client";
import { KioskApp } from "./index";
import { executeScan, executeSetDueDate } from "../machine/session-machine";
import { setKioskConfig } from "../lib/kiosk-config";
import { resetConnectivityForTesting } from "../lib/connectivity";

const source = vi.hoisted(() => ({ listeners: new Set<(event: ScanEvent) => void>() }));
vi.mock("../lib/scan", () => ({
  useScanRouter: () => ({
    subscribe: (listener: (event: ScanEvent) => void) => {
      source.listeners.add(listener);
      return () => source.listeners.delete(listener);
    },
    start: async () => {}, stop: async () => {},
    getRouter: () => ({ getRegisteredSources: () => [] }),
  }),
}));
vi.mock("../machine/session-machine", async (original: <T>() => Promise<T>) => ({
  ...await original<typeof import("../machine/session-machine")>(),
  executeScan: vi.fn(), executeClose: vi.fn().mockResolvedValue(undefined), executeSetDueDate: vi.fn(),
}));

function response(kind: ScanResult["outcome"]["kind"], n?: number, userKnown = true): ScanResult {
  const device = n ? { id: `device-${n}`, name: `Item ${n}`, assetTag: `ASSET-${n}` } : undefined;
  return {
    session: {
      id: "session", kioskId: "kiosk", state: userKnown ? "ready" : "awaiting_user",
      ...(userKnown ? { user: { id: "user", fullName: "Borrower", department: "Ward", openLoanCount: 0 } } : { pendingDevice: device }),
      startedAt: new Date().toISOString(), expiresAt: new Date(Date.now() + 60000).toISOString(),
    },
    outcome: { kind, device }, openLoans: [],
    message: { key: kind === "rejected" ? "device_reserved" : kind, title: "Reserved", detail: "Reserved for another user", tone: "warning" },
  };
}

async function scan(token: string) {
  await act(async () => {
    source.listeners.forEach((listener) => listener({ kind: "scan", scan: { token, source: "manual", scannedAt: new Date().toISOString() } }));
  });
}

beforeEach(() => {
  localStorage.clear(); sessionStorage.clear(); source.listeners.clear(); vi.clearAllMocks();
  vi.mocked(executeScan).mockReset();
  vi.mocked(executeSetDueDate).mockReset();
  resetConnectivityForTesting(false);
  setKioskConfig({ kioskId: "kiosk", kioskName: "Ward", token: "token", defaultLocale: "en", enabledSources: ["manual"] });
});

describe("Finish and Close session", () => {
  it("keeps all device-first scans in a list and returns the borrower's item after identification", async () => {
    const pendingReturn = response("device_pending", 1, false);
    pendingReturn.outcome.device!.status = "on_loan";
    vi.mocked(executeScan).mockResolvedValueOnce(pendingReturn)
      .mockResolvedValueOnce(response("device_pending", 2, false))
      .mockResolvedValueOnce(response("borrowed", 2))
      .mockResolvedValueOnce(response("returned", 1));
    const { container } = render(<KioskApp />);
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Start" })); });
    await scan("device-1");
    expect(screen.getByRole("heading", { name: "Now scan your ID card" })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "This device is already on loan" })).not.toBeInTheDocument();
    await scan("device-2");
    const pending = screen.getByRole("list", { name: "Scanned items" });
    expect(within(pending).getAllByRole("listitem")).toHaveLength(2);
    expect(within(pending).getAllByRole("listitem")[0]).toHaveTextContent("Item 1");
    expect(within(pending).getAllByRole("listitem")[0]).toHaveTextContent("ASSET-1");
    expect(within(pending).getAllByText("Awaiting ID")).toHaveLength(2);
    expect(await axe(container)).toHaveNoViolations();
    await scan("card");
    const rows = within(screen.getByRole("list", { name: "Scanned items" })).getAllByRole("listitem");
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveTextContent("Item 1");
    expect(rows[0]).toHaveTextContent("Returned");
    expect(rows[1]).toHaveTextContent("Item 2");
    expect(rows[1]).toHaveTextContent("Borrowed");
    expect(screen.queryByTestId("success-screen")).not.toBeInTheDocument();
  });

  it("keeps user-first results and refusals inline with return-date controls for earlier borrows", async () => {
    const originalDue = new Date(Date.now() + 86400000).toISOString();
    const changedDue = new Date(Date.now() + 2 * 86400000).toISOString();
    const latestReturn = new Date(Date.now() + 7 * 86400000).toISOString();
    const firstBorrow = response("borrowed", 1);
    firstBorrow.outcome = { ...firstBorrow.outcome, loanId: "loan-1", dueAt: originalDue, latestReturnAt: latestReturn };
    const secondBorrow = response("borrowed", 2);
    secondBorrow.outcome = { ...secondBorrow.outcome, loanId: "loan-2", dueAt: originalDue };
    vi.mocked(executeScan).mockResolvedValueOnce(response("user_identified"))
      .mockResolvedValueOnce(firstBorrow).mockResolvedValueOnce(secondBorrow)
      .mockResolvedValueOnce(response("rejected", 3));
    vi.mocked(executeSetDueDate).mockResolvedValue({ ok: true, loanId: "loan-1", dueAt: changedDue, latestReturnAt: latestReturn, sessionExpiresAt: new Date(Date.now() + 60000).toISOString() });
    const { container } = render(<KioskApp />);
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Start" })); });
    await scan("card"); await scan("device-1"); await scan("device-2"); await scan("device-3");
    const list = screen.getByRole("list", { name: "Scanned items" });
    const rows = within(list).getAllByRole("listitem");
    expect(rows).toHaveLength(3);
    expect(rows[0]).toHaveTextContent("Borrowed");
    expect(rows[1]).toHaveTextContent("Borrowed");
    expect(rows[2]).toHaveTextContent("Failed");
    expect(rows[2]).toHaveTextContent(/reserved/i);
    expect(screen.queryByTestId("blocked-screen")).not.toBeInTheDocument();
    expect(within(rows[0]).getByRole("region", { name: "Return by · ASSET-1" })).toBeVisible();
    expect(within(rows[1]).getByRole("region", { name: "Return by · ASSET-2" })).toBeVisible();
    expect(screen.getByTestId("empty-loans-container")).toBeVisible();
    expect(await axe(container)).toHaveNoViolations();
    const current = within(rows[0]).getByTestId("return-by-current").textContent;
    await act(async () => { fireEvent.click(within(rows[0]).getByRole("button", { name: "Tomorrow 17:00" })); });
    const updated = within(rows[0]).getByTestId("return-by-current").textContent;
    expect(updated).not.toBe(current);
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Finish" })); });
    const summary = screen.getAllByRole("listitem");
    expect(summary[0]).toHaveTextContent("Item 1");
    expect(summary[0]).toHaveTextContent(updated!);
    expect(summary[2]).toHaveTextContent("Failed");
  });

  it("shows every transaction and its failure reason, then closes to a clean kiosk", async () => {
    vi.mocked(executeScan).mockResolvedValueOnce(response("user_identified"))
      .mockResolvedValueOnce(response("borrowed", 1))
      .mockResolvedValueOnce(response("rejected", 2));
    const { container } = render(<KioskApp />);
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Start" })); });
    await scan("card"); await scan("device-1"); await scan("device-2");
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Finish" })); });
    expect(screen.getByRole("heading", { name: "Session summary" })).toBeInTheDocument();
    const rows = screen.getAllByRole("listitem");
    expect(rows).toHaveLength(2);
    expect(within(rows[0]).getByText("Borrowed")).toBeInTheDocument();
    expect(within(rows[1]).getByText("Failed")).toBeInTheDocument();
    expect(rows[1]).toHaveTextContent(/Reserved/i);
    expect(await axe(container)).toHaveNoViolations();
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Close session" })); });
    expect(screen.getByRole("button", { name: "Start" })).toBeInTheDocument();
    expect(screen.queryByText("Borrower")).not.toBeInTheDocument();
    expect(screen.queryByText("Item 1")).not.toBeInTheDocument();
  });

  it("shows devices without an identified user as failed when finished", async () => {
    vi.mocked(executeScan).mockResolvedValueOnce(response("device_pending", 1, false));
    render(<KioskApp />);
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Start" })); });
    await scan("device-1");
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Finish" })); });
    expect(screen.getByRole("listitem")).toHaveTextContent("Failed");
    expect(screen.getByRole("listitem")).toHaveTextContent("No user identified");
  });

  it("includes an unreadable scanner code as a failed attempt", async () => {
    render(<KioskApp />);
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Start" })); });
    await act(async () => {
      source.listeners.forEach((listener) => listener({ kind: "invalid", raw: "unreadable", reason: "invalid-format" }));
    });
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Finish" })); });
    expect(screen.getByRole("listitem")).toHaveTextContent("Failed");
    expect(screen.getByRole("listitem")).toHaveTextContent(/scanned code format is not recognised/i);
  });

  it("allows Finish while offline so failed attempts can still be reviewed", async () => {
    vi.mocked(executeScan).mockResolvedValueOnce(response("user_identified"))
      .mockRejectedValueOnce(new TypeError("Network unavailable"));
    render(<KioskApp />);
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Start" })); });
    await scan("card"); await scan("device-1");
    await act(async () => { resetConnectivityForTesting(true); });
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Finish" })); });
    expect(screen.getByRole("heading", { name: "Session summary" })).toBeInTheDocument();
    expect(screen.getByRole("listitem")).toHaveTextContent("Failed");
  });
});
