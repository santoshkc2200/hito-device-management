import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import {
  CameraSource,
  CAMERA_SUPPORTED_FORMATS,
  DEFAULT_DECODE_INTERVAL_MS,
  DEFAULT_CAMERA_IDLE_TIMEOUT_MS,
} from "./camera";
import { ScanRouter } from "./router";
import type { ScanEvent } from "./types";

interface MockTrack {
  kind: string;
  stop: ReturnType<typeof vi.fn>;
  getCapabilities?: ReturnType<typeof vi.fn>;
  applyConstraints?: ReturnType<typeof vi.fn>;
}

function createMockStream(hasTorch = false): {
  stream: MediaStream;
  tracks: MockTrack[];
} {
  const videoTrack: MockTrack = {
    kind: "video",
    stop: vi.fn(),
    getCapabilities: vi.fn(() => (hasTorch ? { torch: true } : {})),
    applyConstraints: vi.fn().mockResolvedValue(undefined),
  };

  const tracks = [videoTrack];
  const stream = {
    getTracks: () => tracks,
    getVideoTracks: () => [videoTrack],
    getAudioTracks: () => [],
  } as unknown as MediaStream;

  return { stream, tracks };
}

describe("CameraSource", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("exports supported formats and default intervals", () => {
    expect(CAMERA_SUPPORTED_FORMATS).toEqual(["qr_code", "data_matrix", "code_128"]);
    expect(DEFAULT_DECODE_INTERVAL_MS).toBe(100);
    expect(DEFAULT_CAMERA_IDLE_TIMEOUT_MS).toBe(60_000);
  });

  it("unavailableWhenGetUserMediaMissing", async () => {
    const mockNav = {} as Navigator;
    const camera = new CameraSource({ targetNavigator: mockNav });

    const available = await camera.isAvailable();
    expect(available).toBe(false);
  });

  it("deniedPermissionReportsUnavailableWithoutThrowing", async () => {
    const mockNav = {
      mediaDevices: {
        getUserMedia: vi.fn(),
      },
      permissions: {
        query: vi.fn().mockResolvedValue({ state: "denied" }),
      },
    } as unknown as Navigator;

    const camera = new CameraSource({ targetNavigator: mockNav });
    const available = await camera.isAvailable();

    expect(available).toBe(false);
    expect(mockNav.permissions.query).toHaveBeenCalledWith({ name: "camera" });
  });

  it("restrictsFormatsToTheThreeConfigured", async () => {
    let capturedFormats: readonly string[] = [];
    const mockDetector = {
      detect: vi.fn().mockResolvedValue([]),
    };

    const detectorFactory = (formats: readonly string[]) => {
      capturedFormats = formats;
      return mockDetector;
    };

    const { stream } = createMockStream();
    const mockNav = {
      mediaDevices: {
        getUserMedia: vi.fn().mockResolvedValue(stream),
      },
    } as unknown as Navigator;

    const camera = new CameraSource({
      targetNavigator: mockNav,
      barcodeDetectorFactory: detectorFactory,
    });

    const emit = vi.fn();
    await camera.start(emit);

    expect(capturedFormats).toEqual(["qr_code", "data_matrix", "code_128"]);

    await camera.stop();
  });

  it("emitsScanOnDetectedBarcode and auto-stops", async () => {
    const mockDetector = {
      detect: vi.fn().mockResolvedValue([
        { rawValue: "HD-U-7K3M9QXA2F-4", format: "qr_code" },
      ]),
    };

    const { stream, tracks } = createMockStream();
    const mockNav = {
      mediaDevices: {
        getUserMedia: vi.fn().mockResolvedValue(stream),
      },
    } as unknown as Navigator;

    const onScan = vi.fn();
    const camera = new CameraSource({
      targetNavigator: mockNav,
      barcodeDetectorFactory: () => mockDetector,
      onScan,
      autoStopOnScan: true,
    });

    const emit = vi.fn();
    await camera.start(emit);

    // Provide readyState to simulated video element
    const video = camera.getVideoElement();
    if (video) {
      Object.defineProperty(video, "readyState", {
        value: HTMLMediaElement.HAVE_CURRENT_DATA,
        configurable: true,
      });
    }

    // Advance timers for decode loop tick
    await vi.advanceTimersByTimeAsync(DEFAULT_DECODE_INTERVAL_MS);

    expect(emit).toHaveBeenCalledWith("HD-U-7K3M9QXA2F-4");
    expect(onScan).toHaveBeenCalledWith("HD-U-7K3M9QXA2F-4");

    // Assert camera stopped and released tracks
    expect(camera.isActive()).toBe(false);
    expect(tracks[0]?.stop).toHaveBeenCalled();
  });

  it("stopReleasesEveryTrack", async () => {
    const { stream, tracks } = createMockStream();
    const mockNav = {
      mediaDevices: {
        getUserMedia: vi.fn().mockResolvedValue(stream),
      },
    } as unknown as Navigator;

    const camera = new CameraSource({ targetNavigator: mockNav });
    await camera.start(vi.fn());

    expect(camera.isActive()).toBe(true);
    expect(tracks[0]?.stop).not.toHaveBeenCalled();

    await camera.stop();

    expect(camera.isActive()).toBe(false);
    expect(tracks[0]?.stop).toHaveBeenCalled();
    expect(camera.getStream()).toBeNull();
  });

  it("hiddenTabStopsTheCamera", async () => {
    const { stream, tracks } = createMockStream();
    const mockNav = {
      mediaDevices: {
        getUserMedia: vi.fn().mockResolvedValue(stream),
      },
    } as unknown as Navigator;

    let visibilityState = "visible";
    const eventListeners: Record<string, (() => void)[]> = {};

    const mockDoc = {
      createElement: (tag: string) => document.createElement(tag),
      get visibilityState() {
        return visibilityState;
      },
      addEventListener: vi.fn((event: string, listener: () => void) => {
        eventListeners[event] = eventListeners[event] || [];
        eventListeners[event].push(listener);
      }),
      removeEventListener: vi.fn((event: string, listener: () => void) => {
        if (eventListeners[event]) {
          eventListeners[event] = eventListeners[event].filter((l) => l !== listener);
        }
      }),
    } as unknown as Document;

    const camera = new CameraSource({
      targetNavigator: mockNav,
      targetDocument: mockDoc,
    });

    await camera.start(vi.fn());
    expect(camera.isActive()).toBe(true);

    // Simulate tab hidden
    visibilityState = "hidden";
    if (eventListeners["visibilitychange"]) {
      eventListeners["visibilitychange"].forEach((fn) => fn());
    }

    expect(camera.isActive()).toBe(false);
    expect(tracks[0]?.stop).toHaveBeenCalled();
  });

  it("idleTimeoutReturnsToScannerMode", async () => {
    const { stream, tracks } = createMockStream();
    const mockNav = {
      mediaDevices: {
        getUserMedia: vi.fn().mockResolvedValue(stream),
      },
    } as unknown as Navigator;

    const onIdleTimeout = vi.fn();
    const camera = new CameraSource({
      targetNavigator: mockNav,
      idleTimeoutMs: 5000,
      onIdleTimeout,
    });

    await camera.start(vi.fn());
    expect(camera.isActive()).toBe(true);

    // Advance timer past idle timeout
    await vi.advanceTimersByTimeAsync(5100);

    expect(onIdleTimeout).toHaveBeenCalled();
    expect(camera.isActive()).toBe(false);
    expect(tracks[0]?.stop).toHaveBeenCalled();
  });

  it("supports torch detection and toggle", async () => {
    const { stream, tracks } = createMockStream(true);
    const mockNav = {
      mediaDevices: {
        getUserMedia: vi.fn().mockResolvedValue(stream),
      },
    } as unknown as Navigator;

    const camera = new CameraSource({ targetNavigator: mockNav });
    await camera.start(vi.fn());

    expect(camera.hasTorch()).toBe(true);
    expect(camera.isTorchOn()).toBe(false);

    const success = await camera.setTorch(true);
    expect(success).toBe(true);
    expect(camera.isTorchOn()).toBe(true);
    expect(tracks[0]?.applyConstraints).toHaveBeenCalledWith({
      advanced: [{ torch: true }],
    });

    await camera.stop();
  });


  it("integrates with ScanRouter as a scan source", async () => {
    const mockDetector = {
      detect: vi.fn().mockResolvedValue([
        { rawValue: "HD-U-B3G6822S6K-H", format: "qr_code" },
      ]),
    };

    const { stream } = createMockStream();
    const mockNav = {
      mediaDevices: {
        getUserMedia: vi.fn().mockResolvedValue(stream),
      },
    } as unknown as Navigator;

    const camera = new CameraSource({
      targetNavigator: mockNav,
      barcodeDetectorFactory: () => mockDetector,
    });

    const router = new ScanRouter();
    router.register(camera);

    await router.start();

    const receivedEvents: ScanEvent[] = [];
    router.subscribe((ev) => receivedEvents.push(ev));

    const video = camera.getVideoElement();
    if (video) {
      Object.defineProperty(video, "readyState", {
        value: HTMLMediaElement.HAVE_CURRENT_DATA,
        configurable: true,
      });
    }

    await vi.advanceTimersByTimeAsync(DEFAULT_DECODE_INTERVAL_MS);

    expect(receivedEvents).toHaveLength(1);
    expect(receivedEvents[0]).toMatchObject({
      kind: "scan",
      scan: {
        token: "HD-U-B3G6822S6K-H",
        source: "camera",
      },
    });

    await router.stop();
  });

  it("defaults to front camera ('user') and can be configured with custom mode", async () => {
    const { stream } = createMockStream();
    const getUserMedia = vi.fn().mockResolvedValue(stream);
    const mockNav = {
      mediaDevices: { getUserMedia },
    } as unknown as Navigator;

    const defaultCamera = new CameraSource({ targetNavigator: mockNav });
    expect(defaultCamera.getFacingMode()).toBe("user");

    await defaultCamera.start(vi.fn());
    expect(getUserMedia).toHaveBeenCalledWith(
      expect.objectContaining({
        video: expect.objectContaining({
          facingMode: "user",
        }),
      })
    );
    await defaultCamera.stop();

    const rearCamera = new CameraSource({
      targetNavigator: mockNav,
      facingMode: "environment",
    });
    expect(rearCamera.getFacingMode()).toBe("environment");

    await rearCamera.start(vi.fn());
    expect(getUserMedia).toHaveBeenLastCalledWith(
      expect.objectContaining({
        video: expect.objectContaining({
          facingMode: "environment",
        }),
      })
    );
    await rearCamera.stop();
  });

  it("switches camera when running, releases old stream and rebinds new stream", async () => {
    const stream1 = createMockStream(false);
    const stream2 = createMockStream(true);

    const getUserMedia = vi
      .fn()
      .mockResolvedValueOnce(stream1.stream)
      .mockResolvedValueOnce(stream2.stream);

    const mockNav = {
      mediaDevices: { getUserMedia },
    } as unknown as Navigator;

    const camera = new CameraSource({ targetNavigator: mockNav });
    expect(camera.getFacingMode()).toBe("user");

    await camera.start(vi.fn());
    expect(camera.hasTorch()).toBe(false);

    // Switch camera from front (user) to rear (environment)
    const newMode = await camera.switchCamera();
    expect(newMode).toBe("environment");
    expect(camera.getFacingMode()).toBe("environment");

    // Check old track was stopped
    expect(stream1.tracks[0]?.stop).toHaveBeenCalled();

    // Check second stream was requested with "environment"
    expect(getUserMedia).toHaveBeenCalledTimes(2);
    expect(getUserMedia).toHaveBeenLastCalledWith(
      expect.objectContaining({
        video: expect.objectContaining({
          facingMode: "environment",
        }),
      })
    );

    // Stream 2 had torch capability
    expect(camera.hasTorch()).toBe(true);

    // Stopping resets to default facing mode
    await camera.stop();
    expect(camera.getFacingMode()).toBe("user");
  });
});
