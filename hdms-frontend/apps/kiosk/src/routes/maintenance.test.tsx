import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { clearMaintenance, reportMaintenance, resetMaintenanceForTesting } from "@hdms/ui";
import { KioskApp } from "./index";
import { getSessionId, setKioskConfig, setSessionId } from "@/lib/kiosk-config";
import { resetConnectivityForTesting } from "@/lib/connectivity";

describe("kiosk under maintenance", () => {
  beforeEach(() => {
    localStorage.clear();
    resetConnectivityForTesting(false);
    resetMaintenanceForTesting(async () => false);
    setKioskConfig({ kioskId: "k1", kioskName: "Ward 3", token: "tok", defaultLocale: "en" });
  });

  afterEach(() => {
    resetMaintenanceForTesting();
    resetConnectivityForTesting(false);
  });

  it("replaces the kiosk with the notice, drops the open session, and returns to idle by itself", async () => {
    render(<KioskApp />);
    expect(screen.getByTestId("start-scanning-button")).toBeInTheDocument();
    setSessionId("session-open");

    act(() => reportMaintenance());

    expect(await screen.findByTestId("maintenance-screen")).toBeInTheDocument();
    expect(screen.queryByTestId("start-scanning-button")).toBeNull();
    expect(getSessionId()).toBeNull();

    act(() => clearMaintenance());
    expect(await screen.findByTestId("start-scanning-button")).toBeInTheDocument();
  });

  it("shows maintenance, not offline, when both apply", async () => {
    render(<KioskApp />);
    act(() => {
      resetConnectivityForTesting(true);
      reportMaintenance();
    });
    expect(await screen.findByTestId("maintenance-screen")).toBeInTheDocument();
    expect(screen.queryByTestId("offline-screen")).toBeNull();
  });
});
