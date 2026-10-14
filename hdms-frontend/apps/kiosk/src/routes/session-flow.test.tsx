import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { axe } from "vitest-axe";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ScanEvent } from "@hdms/scan";
import type { ScanResult } from "@hdms/api-client";
import { KioskApp } from "./index";
import { executeScan } from "../machine/session-machine";
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
  executeScan: vi.fn(), executeClose: vi.fn().mockResolvedValue(undefined),
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
  resetConnectivityForTesting(false);
  setKioskConfig({ kioskId: "kiosk", kioskName: "Ward", token: "token", defaultLocale: "en", enabledSources: ["manual"] });
});

describe("Finish and Close session", () => {
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
