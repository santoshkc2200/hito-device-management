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

    expect(registered).toHaveLength(2);
    expect(registered.map((s) => s.id)).toEqual(["scanner", "camera"]);

    const listener = vi.fn();
    const unsubscribe = result.current.subscribe(listener);
    expect(typeof unsubscribe).toBe("function");

    unsubscribe();
    unmount();
  });
});
