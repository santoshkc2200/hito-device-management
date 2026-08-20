import { describe, expect, it, vi } from "vitest";
import { ManualSource } from "./manual";
import { ScanRouter } from "./router";
import type { ScanEvent } from "./types";

describe("ManualSource", () => {
  it("is always available", async () => {
    const source = new ManualSource();
    expect(await source.isAvailable()).toBe(true);
    expect(source.id).toBe("manual");
    expect(source.label).toBe("Attendant Manual Entry");
  });

  it("customizes label when options provided", () => {
    const source = new ManualSource({ label: "Custom Keypad" });
    expect(source.label).toBe("Custom Keypad");
  });

  it("does not emit before start() is called", () => {
    const source = new ManualSource();
    const result = source.submit("HD-U-B3G6822S6K-H");
    expect(result).toBe(false);
  });

  it("emits token when active and returns true", async () => {
    const source = new ManualSource();
    const emitSpy = vi.fn();

    await source.start(emitSpy);
    const result = source.submit("HD-U-B3G6822S6K-H");

    expect(result).toBe(true);
    expect(emitSpy).toHaveBeenCalledWith("HD-U-B3G6822S6K-H");

    await source.stop();
    expect(source.submit("HD-U-B3G6822S6K-H")).toBe(false);
  });

  it("integrates with ScanRouter as a scan source and tags source as manual", async () => {
    const router = new ScanRouter({ debounceMs: 500 });
    const source = new ManualSource();
    router.register(source);

    const receivedEvents: ScanEvent[] = [];
    router.subscribe((event) => receivedEvents.push(event));

    await router.start();

    // Submit valid token
    const emitted = source.submit("HD-U-B3G6822S6K-H");
    expect(emitted).toBe(true);

    const scanEvent = receivedEvents.find((e) => e.kind === "scan");
    expect(scanEvent).toBeDefined();
    if (scanEvent && scanEvent.kind === "scan") {
      expect(scanEvent.scan.token).toBe("HD-U-B3G6822S6K-H");
      expect(scanEvent.scan.source).toBe("manual");
    }

    await router.stop();
  });


  it("routes invalid tokens as kind: invalid through ScanRouter", async () => {
    const router = new ScanRouter();
    const source = new ManualSource();
    router.register(source);

    const receivedEvents: ScanEvent[] = [];
    router.subscribe((event) => receivedEvents.push(event));

    await router.start();

    // Submit invalid token
    source.submit("HD-U-INVALID-TOKEN-9");

    const invalidEvent = receivedEvents.find((e) => e.kind === "invalid");
    expect(invalidEvent).toBeDefined();
    if (invalidEvent && invalidEvent.kind === "invalid") {
      expect(invalidEvent.raw).toBe("HD-U-INVALID-TOKEN-9");
    }

    await router.stop();
  });
});
