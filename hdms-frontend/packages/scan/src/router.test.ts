import { describe, it, expect, beforeEach } from "vitest";
import { ScanRouter } from "./router";
import { FakeSource } from "./fake-source";
import type { ScanEvent } from "./types";

// Valid golden tokens from fixtures/token-fixtures.json
const VALID_USER_TOKEN = "HD-U-B3G6822S6K-H";
const VALID_DEVICE_TOKEN = "HD-D-BRH8VFTBAA-7";

describe("ScanRouter", () => {
  let router: ScanRouter;
  let fakeNow: number;

  beforeEach(() => {
    fakeNow = 1000;
    router = new ScanRouter({
      now: () => fakeNow,
      debounceMs: 1500,
    });
  });

  it("emitsOneScanPerSource", async () => {
    const source = new FakeSource("scanner", "Bluetooth Scanner");
    router.register(source);
    await router.start();

    const events: ScanEvent[] = [];
    router.subscribe((e) => events.push(e));

    source.simulateScan(VALID_USER_TOKEN);

    const scanEvents = events.filter((e) => e.kind === "scan");
    expect(scanEvents).toHaveLength(1);
    expect(scanEvents[0]).toEqual({
      kind: "scan",
      scan: {
        token: VALID_USER_TOKEN,
        source: "scanner",
        scannedAt: expect.any(String),
        raw: VALID_USER_TOKEN,
      },
    });
  });

  it("debouncesRepeatWithin1500ms", async () => {
    const source = new FakeSource("scanner", "Bluetooth Scanner");
    router.register(source);
    await router.start();

    const events: ScanEvent[] = [];
    router.subscribe((e) => events.push(e));

    source.simulateScan(VALID_USER_TOKEN);
    fakeNow += 500; // 500ms elapsed (< 1500ms)
    source.simulateScan(VALID_USER_TOKEN);

    expect(events).toHaveLength(2);
    const firstEvent = events[0];
    expect(firstEvent?.kind).toBe("scan");
    expect(events[1]).toEqual({
      kind: "duplicate",
      token: VALID_USER_TOKEN,
    });
  });

  it("allowsSameTokenAfterWindow", async () => {
    const source = new FakeSource("scanner", "Bluetooth Scanner");
    router.register(source);
    await router.start();

    const events: ScanEvent[] = [];
    router.subscribe((e) => events.push(e));

    source.simulateScan(VALID_USER_TOKEN);
    fakeNow += 1500; // Exact/after debounce window
    source.simulateScan(VALID_USER_TOKEN);

    const scans = events.filter((e) => e.kind === "scan");
    expect(scans).toHaveLength(2);
  });

  it("debounceIsCaseAndWhitespaceInsensitive", async () => {
    const source = new FakeSource("scanner", "Bluetooth Scanner");
    router.register(source);
    await router.start();

    const events: ScanEvent[] = [];
    router.subscribe((e) => events.push(e));

    // Scans with whitespace and lowercase
    source.simulateScan(`  ${VALID_USER_TOKEN.toLowerCase()}  `);
    fakeNow += 400;
    source.simulateScan(VALID_USER_TOKEN);

    expect(events).toHaveLength(2);
    const first = events[0];
    expect(first?.kind).toBe("scan");
    if (first && first.kind === "scan") {
      expect(first.scan.token).toBe(VALID_USER_TOKEN);
    }
    expect(events[1]).toEqual({
      kind: "duplicate",
      token: VALID_USER_TOKEN,
    });
  });

  it("invalidTokenNeverReachesSubscribersAsScan", async () => {
    const source = new FakeSource("scanner", "Bluetooth Scanner");
    router.register(source);
    await router.start();

    const events: ScanEvent[] = [];
    router.subscribe((e) => events.push(e));

    // Garbage barcode (e.g. standard UPC or invalid namespace)
    source.simulateScan("123456789012");

    expect(events).toHaveLength(1);
    expect(events[0]).toEqual({
      kind: "invalid",
      raw: "123456789012",
      reason: "invalid-format",
    });

    // Checksum failure token (tampered check char)
    const badChecksumToken = `${VALID_USER_TOKEN.slice(0, -1)}9`;
    source.simulateScan(badChecksumToken);
    expect(events).toHaveLength(2);
    expect(events[1]).toEqual({
      kind: "invalid",
      raw: badChecksumToken,
      reason: "invalid-checksum",
    });
  });

  it("twoSourcesInterleaveIntoOneOrderedStream", async () => {
    const scanner = new FakeSource("scanner", "Scanner");
    const camera = new FakeSource("camera", "Camera");
    router.register(scanner);
    router.register(camera);
    await router.start();

    const events: ScanEvent[] = [];
    router.subscribe((e) => events.push(e));

    scanner.simulateScan(VALID_USER_TOKEN);
    fakeNow += 200;
    camera.simulateScan(VALID_DEVICE_TOKEN);

    const scans = events.filter((e) => e.kind === "scan");
    expect(scans).toHaveLength(2);
    const scan0 = scans[0];
    const scan1 = scans[1];
    if (scan0 && scan0.kind === "scan") {
      expect(scan0.scan.source).toBe("scanner");
    }
    if (scan1 && scan1.kind === "scan") {
      expect(scan1.scan.source).toBe("camera");
    }
  });

  it("aThrowingSourceIsIsolated", async () => {
    const goodSource = new FakeSource("good", "Good Source");
    const faultySource = new FakeSource("faulty", "Faulty Source");
    router.register(goodSource);
    router.register(faultySource);
    await router.start();

    const events: ScanEvent[] = [];
    router.subscribe((e) => events.push(e));

    // Faulty source throws during emission
    faultySource.simulateError(new Error("Hardware disconnected unexpectedly"));

    // Router survives and marks faulty source unavailable
    const stateEvents = events.filter((e) => e.kind === "source-state");
    expect(stateEvents.some((e) => e.id === "faulty" && !e.available)).toBe(
      true
    );

    // Good source continues to function normally
    goodSource.simulateScan(VALID_USER_TOKEN);
    const scans = events.filter((e) => e.kind === "scan");
    expect(scans).toHaveLength(1);
    const scan0 = scans[0];
    if (scan0 && scan0.kind === "scan") {
      expect(scan0.scan.source).toBe("good");
    }
  });


  it("stopDetachesEverySource", async () => {
    const s1 = new FakeSource("s1", "S1");
    const s2 = new FakeSource("s2", "S2");
    router.register(s1);
    router.register(s2);

    await router.start();
    expect(s1.started).toBe(true);
    expect(s2.started).toBe(true);

    await router.stop();
    expect(s1.stopped).toBe(true);
    expect(s1.stopCount).toBe(1);
    expect(s2.stopped).toBe(true);
    expect(s2.stopCount).toBe(1);
  });
});
