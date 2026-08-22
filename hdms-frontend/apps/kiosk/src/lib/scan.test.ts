import { renderHook } from "@testing-library/react";
import { describe, it, expect, beforeEach, vi } from "vitest";
import { useScanRouter } from "./scan";
import { setKioskConfig } from "./kiosk-config";

describe("useScanRouter", () => {
  beforeEach(() => {
    localStorage.clear();
    setKioskConfig({
      kioskId: "kiosk-01",
      kioskName: "Front Desk Kiosk",
      token: "test-token",
      enabledSources: ["hid", "camera"],
    });
  });

  it("registers enabled sources from kiosk config and manages lifecycle", async () => {
    const { result, unmount } = renderHook(() => useScanRouter());

    const router = result.current.getRouter();
    const registered = router.getRegisteredSources();

    expect(registered).toHaveLength(1);
    expect(registered.map((s) => s.id)).toEqual(["scanner"]);

    // Camera stays unregistered and lazy. CameraOverlay starts it only after
    // explicit fallback use, so startup cannot request camera permission.
    expect(registered.some((s) => s.id === "camera")).toBe(false);

    const listener = vi.fn();
    const unsubscribe = result.current.subscribe(listener);
    expect(typeof unsubscribe).toBe("function");

    unsubscribe();
    unmount();
  });
});
