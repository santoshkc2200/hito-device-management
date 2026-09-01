import { render, screen, waitFor, act } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { App } from "./App";
import * as apiClient from "@hdms/api-client";
import {
  clearKioskConfig,
  getKioskConfig,
  setKioskConfig,
} from "./lib/kiosk-config";
import {
  setServiceWorkerUpdatePending,
  isServiceWorkerUpdatePending,
  useDeferredServiceWorkerUpdate,
  resetServiceWorkerUpdateForTesting,
} from "./lib/sw-update";
import { pwaOptions } from "../vite.config";

vi.mock("@hdms/api-client", async () => {
  const actual = await vi.importActual<typeof apiClient>("@hdms/api-client");
  return {
    ...actual,
    pairKiosk: vi.fn(),
    createSession: vi.fn(),
    submitScan: vi.fn(),
  };
});

describe("PWA and Kiosk Mode (Phase 3.7)", () => {
  beforeEach(() => {
    clearKioskConfig();
    resetServiceWorkerUpdateForTesting();
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("pairingExchangeStoresTokenAndEntersIdle", async () => {
    const user = userEvent.setup();

    vi.mocked(apiClient.pairKiosk).mockResolvedValueOnce({
      data: {
        kioskId: "kiosk-er-01",
        name: "Emergency Room Kiosk",
        token: "kiosk-bearer-tok-8888",
        defaultLocale: "en",
      },
      error: undefined,
      request: new Request("http://localhost/v1/kiosks/pair"),
      response: new Response(JSON.stringify({}), { status: 200 }),
    });

    render(<App />);

    expect(
      await screen.findByTestId("pairing-title")
    ).toBeInTheDocument();

    // Type 6 digits on keypad
    for (let i = 1; i <= 6; i++) {
      await user.click(screen.getByTestId(`pairing-key-${i}`));
    }

    // Submit pairing
    await user.click(screen.getByTestId("pairing-submit-button"));

    // Verify token stored in localStorage
    await waitFor(() => {
      const cfg = getKioskConfig();
      expect(cfg?.kioskId).toBe("kiosk-er-01");
      expect(cfg?.kioskName).toBe("Emergency Room Kiosk");
      expect(cfg?.token).toBe("kiosk-bearer-tok-8888");
    });

    // Verify transition into Idle screen
    expect(
      await screen.findByText(/Emergency Room Kiosk/i)
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        /Tap Start, then scan your staff ID card or a device barcode in any order\./i
      )
    ).toBeInTheDocument();
  });

  it("unauthorizedResponseReturnsToPairing", async () => {
    setKioskConfig({
      kioskId: "kiosk-er-01",
      kioskName: "Emergency Room Kiosk",
      token: "revoked-bearer-tok",
    });

    render(<App />);

    expect(
      await screen.findByText(/Emergency Room Kiosk/i)
    ).toBeInTheDocument();

    // Simulate 401 response from an API call
    const unauthResponse = new Response(
      JSON.stringify({
        type: "https://api.hdms.hito.internal/problems/unauthorized",
        title: "Unauthorized",
        status: 401,
        detail: "Kiosk token revoked or expired.",
      }),
      {
        status: 401,
        headers: { "Content-Type": "application/problem+json" },
      }
    );

    // Trigger API response interceptor
    for (const fn of apiClient.client.interceptors.response.fns) {
      if (fn) {
        await fn(unauthResponse, new Request("http://localhost/v1/sessions"), {} as any);
      }
    }

    // Verify config cleared
    expect(getKioskConfig()).toBeNull();

    // Verify UI transitioned to PairingScreen
    expect(
      await screen.findByTestId("pairing-title")
    ).toBeInTheDocument();
  });

  it("tokenNeverAppearsInDomOrConsole", async () => {
    const SECRET_TOKEN = "SUPER_SECRET_TOKEN_XYZ_99999";

    const consoleLogSpy = vi.spyOn(console, "log").mockImplementation(() => {});
    const consoleInfoSpy = vi.spyOn(console, "info").mockImplementation(() => {});
    const consoleWarnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
    const consoleErrorSpy = vi.spyOn(console, "error").mockImplementation(() => {});

    setKioskConfig({
      kioskId: "kiosk-01",
      kioskName: "Secure Kiosk",
      token: SECRET_TOKEN,
      defaultLocale: "en",
    });

    const { container } = render(<App />);

    expect(
      await screen.findByText(
        /Tap Start, then scan your staff ID card or a device barcode in any order\./i
      )
    ).toBeInTheDocument();

    // Verify token does not appear in DOM HTML
    expect(container.innerHTML).not.toContain(SECRET_TOKEN);

    // Verify token does not appear in any captured console logs
    const allConsoleCalls = [
      ...consoleLogSpy.mock.calls,
      ...consoleInfoSpy.mock.calls,
      ...consoleWarnSpy.mock.calls,
      ...consoleErrorSpy.mock.calls,
    ];

    for (const call of allConsoleCalls) {
      const formatted = JSON.stringify(call);
      expect(formatted).not.toContain(SECRET_TOKEN);
    }
  });

  it("serviceWorkerUpdateDeferredWhileSessionActive", () => {
    const reloadMock = vi.fn();

    // Test helper component
    function TestSWConsumer({ state }: { state: any }) {
      useDeferredServiceWorkerUpdate(state);
      return <div data-testid="state-display">{state}</div>;
    }

    const { rerender } = render(<TestSWConsumer state="awaiting_device" />);

    // Trigger an update while machine is active in awaiting_device state
    act(() => {
      setServiceWorkerUpdatePending(true, reloadMock);
    });

    expect(isServiceWorkerUpdatePending()).toBe(true);
    // Reload must NOT happen while session is active
    expect(reloadMock).not.toHaveBeenCalled();

    // Machine transitions to ready / active
    rerender(<TestSWConsumer state="ready" />);
    expect(reloadMock).not.toHaveBeenCalled();

    // Machine transitions to idle (user finished / closed session)
    rerender(<TestSWConsumer state="idle" />);

    // Now reload MUST be triggered immediately
    expect(reloadMock).toHaveBeenCalledTimes(1);
    expect(isServiceWorkerUpdatePending()).toBe(false);
  });

  it("apiResponsesAreNotCached", () => {
    expect(pwaOptions).toBeDefined();

    const workbox = pwaOptions.workbox ?? {};

    // Verify workbox precache includes wasm, html, js, css, png, svg
    const globPatterns = (workbox.globPatterns ?? []).join(" ");
    expect(globPatterns).toContain("wasm");
    expect(globPatterns).toContain("html");
    expect(globPatterns).toContain("js");

    // Verify runtimeCaching specifies NetworkOnly for /v1/*
    const runtimeCaching = workbox.runtimeCaching ?? [];
    const v1RouteCache = runtimeCaching.find((rc: any) =>
      rc.urlPattern && rc.urlPattern.toString().includes("v1")
    );

    expect(v1RouteCache).toBeDefined();
    expect(v1RouteCache?.handler).toBe("NetworkOnly");
  });
});
