import { BarcodeDetector } from "barcode-detector";
import type { Scan, ScanSource } from "./types";

/**
 * Standard barcode and 2D code formats recognized by HDMS Kiosk.
 * Strictly restricted to prevent wasted CPU cycles on irrelevant formats.
 */
export const CAMERA_SUPPORTED_FORMATS = [
  "qr_code",
  "data_matrix",
  "code_128",
] as const;

export type SupportedBarcodeFormat = (typeof CAMERA_SUPPORTED_FORMATS)[number];

/**
 * Default interval between frame decodes (in milliseconds).
 * 100ms ≈ 10 frames per second, balancing decoding latency with thermal/battery budget.
 */
export const DEFAULT_DECODE_INTERVAL_MS = 100;

/**
 * Maximum duration the camera may remain active with no barcode detected (in milliseconds).
 * After 60 seconds of idle scanning, the camera turns off automatically.
 */
export const DEFAULT_CAMERA_IDLE_TIMEOUT_MS = 60_000;

export type BarcodeDetectorSource =
  | HTMLVideoElement
  | HTMLCanvasElement
  | ImageBitmap;

export interface CameraSourceOptions {
  label?: string;
  decodeIntervalMs?: number;
  idleTimeoutMs?: number;
  autoStopOnScan?: boolean;
  targetDocument?: Document;
  targetNavigator?: Navigator;
  barcodeDetectorFactory?: (formats: readonly string[]) => {
    detect: (source: BarcodeDetectorSource) => Promise<Array<{ rawValue: string; format?: string }>>;
  };
  onScan?: (token: string) => void;
  onIdleTimeout?: () => void;
  onError?: (error: Error) => void;
}

export class CameraSource implements ScanSource {
  readonly id = "camera" as const;
  readonly label: string;

  private decodeIntervalMs: number;
  private idleTimeoutMs: number;
  private autoStopOnScan: boolean;
  private targetDocument: Document | null;
  private targetNavigator: Navigator | null;
  private detectorFactory?: CameraSourceOptions["barcodeDetectorFactory"];

  private emitCallback: ((rawOrScan: string | Scan) => void) | null = null;
  private onScanCallback?: (token: string) => void;
  private onIdleTimeoutCallback?: () => void;
  private onErrorCallback?: (error: Error) => void;

  private stream: MediaStream | null = null;
  private videoElement: HTMLVideoElement | null = null;
  private isRunning = false;
  private isTorchActive = false;
  private decodeTimer: ReturnType<typeof setTimeout> | null = null;
  private idleTimer: ReturnType<typeof setTimeout> | null = null;
  private visibilityHandler: (() => void) | null = null;
  private lastError: Error | null = null;

  constructor(options?: CameraSourceOptions) {
    this.label = options?.label ?? "Camera Scanner";
    this.decodeIntervalMs = options?.decodeIntervalMs ?? DEFAULT_DECODE_INTERVAL_MS;
    this.idleTimeoutMs = options?.idleTimeoutMs ?? DEFAULT_CAMERA_IDLE_TIMEOUT_MS;
    this.autoStopOnScan = options?.autoStopOnScan ?? true;
    this.targetDocument =
      options?.targetDocument ?? (typeof document !== "undefined" ? document : null);
    this.targetNavigator =
      options?.targetNavigator ?? (typeof navigator !== "undefined" ? navigator : null);
    this.detectorFactory = options?.barcodeDetectorFactory;
    this.onScanCallback = options?.onScan;
    this.onIdleTimeoutCallback = options?.onIdleTimeout;
    this.onErrorCallback = options?.onError;
  }

  /**
   * Checks whether the camera hardware and getUserMedia API are available,
   * and ensures camera permission has not been explicitly denied.
   */
  async isAvailable(): Promise<boolean> {
    try {
      const nav = this.targetNavigator;
      if (!nav?.mediaDevices?.getUserMedia) {
        return false;
      }

      // Check permission if Permissions API is supported
      if (nav.permissions?.query) {
        try {
          const status = await nav.permissions.query({ name: "camera" as PermissionName });
          if (status.state === "denied") {
            return false;
          }
        } catch {
          // Permissions API for camera name query may not be supported on all browsers (e.g. Safari);
          // fall back to assuming available if getUserMedia exists.
        }
      }

      return true;
    } catch {
      return false;
    }
  }

  /**
   * Starts the video stream, attaches inline playback, and begins the frame decode loop.
   */
  async start(emit: (rawOrScan: string | Scan) => void): Promise<void> {
    if (this.isRunning) {
      this.emitCallback = emit;
      return;
    }

    this.emitCallback = emit;
    this.lastError = null;

    const nav = this.targetNavigator;
    if (!nav?.mediaDevices?.getUserMedia) {
      const err = new Error("getUserMedia is not supported on this platform");
      this.lastError = err;
      this.onErrorCallback?.(err);
      throw err;
    }

    try {
      const stream = await nav.mediaDevices.getUserMedia({
        video: {
          facingMode: "environment",
          width: { ideal: 1280 },
          height: { ideal: 720 },
        },
        audio: false,
      });

      this.stream = stream;

      // Create or reuse video element configured for iOS Safari inline playback
      let video = this.videoElement;
      if (!video && this.targetDocument) {
        video = this.targetDocument.createElement("video");
        this.videoElement = video;
      }

      if (video) {
        video.setAttribute("playsinline", "true");
        video.playsInline = true;
        video.muted = true;
        video.autoplay = true;
        video.srcObject = stream;
        try {
          await video.play();
        } catch {
          // Ignore play interruption errors
        }
      }

      this.isRunning = true;

      // Setup BarcodeDetector
      const detector = this.detectorFactory
        ? this.detectorFactory(CAMERA_SUPPORTED_FORMATS)
        : new BarcodeDetector({ formats: [...CAMERA_SUPPORTED_FORMATS] });

      // Start decoding loop
      this.startDecodeLoop(detector);

      // Start idle watchdog timer
      this.resetIdleTimer();

      // Listen to tab visibility changes to stop camera when hidden
      if (this.targetDocument) {
        this.visibilityHandler = () => {
          if (this.targetDocument?.visibilityState === "hidden") {
            void this.stop();
          }
        };
        this.targetDocument.addEventListener(
          "visibilitychange",
          this.visibilityHandler
        );
      }
    } catch (err) {
      const error = err instanceof Error ? err : new Error(String(err));
      this.lastError = error;
      this.onErrorCallback?.(error);
      await this.stop();
      throw error;
    }
  }

  /**
   * Stops all active tracks, clears timers, detaches listeners and tears down the video stream.
   */
  async stop(): Promise<void> {
    this.isRunning = false;
    this.isTorchActive = false;

    // 1. Clear decode timer and idle watchdog
    if (this.decodeTimer !== null) {
      clearTimeout(this.decodeTimer);
      this.decodeTimer = null;
    }
    if (this.idleTimer !== null) {
      clearTimeout(this.idleTimer);
      this.idleTimer = null;
    }

    // 2. Remove document visibility listener
    if (this.targetDocument && this.visibilityHandler) {
      this.targetDocument.removeEventListener(
        "visibilitychange",
        this.visibilityHandler
      );
      this.visibilityHandler = null;
    }

    // 3. Stop every media track
    if (this.stream) {
      for (const track of this.stream.getTracks()) {
        try {
          track.stop();
        } catch {
          // Ignore
        }
      }
      this.stream = null;
    }

    // 4. Reset video element
    if (this.videoElement) {
      try {
        this.videoElement.pause();
        this.videoElement.srcObject = null;
      } catch {
        // Ignore
      }
    }

    this.emitCallback = null;
  }

  /**
   * Returns true if any active video track supports the torch constraint.
   */
  hasTorch(): boolean {
    if (!this.stream) return false;
    const videoTrack = this.stream.getVideoTracks()[0];
    if (!videoTrack) return false;

    // Check track capabilities
    if (typeof videoTrack.getCapabilities === "function") {
      const caps = videoTrack.getCapabilities() as { torch?: boolean };
      return Boolean(caps?.torch);
    }
    return false;
  }

  /**
   * Returns whether the torch is currently active.
   */
  isTorchOn(): boolean {
    return this.isTorchActive;
  }

  /**
   * Toggles the torch if supported by the active camera track.
   */
  async setTorch(enabled: boolean): Promise<boolean> {
    if (!this.stream) return false;
    const videoTrack = this.stream.getVideoTracks()[0];
    if (!videoTrack) return false;

    if (!this.hasTorch()) {
      return false;
    }

    try {
      await videoTrack.applyConstraints({
        advanced: [{ torch: enabled } as MediaTrackConstraintSet],
      });
      this.isTorchActive = enabled;
      return true;
    } catch {
      return false;
    }
  }

  isActive(): boolean {
    return this.isRunning;
  }

  getVideoElement(): HTMLVideoElement | null {
    return this.videoElement;
  }

  getStream(): MediaStream | null {
    return this.stream;
  }

  getLastError(): Error | null {
    return this.lastError;
  }

  private startDecodeLoop(detector: {
    detect: (source: BarcodeDetectorSource) => Promise<Array<{ rawValue: string; format?: string }>>;
  }): void {
    const loop = async () => {
      if (!this.isRunning) return;

      const video = this.videoElement;
      if (video && video.readyState >= HTMLMediaElement.HAVE_CURRENT_DATA) {
        try {
          const barcodes = await detector.detect(video);
          const firstBarcode = barcodes?.[0];
          if (firstBarcode && firstBarcode.rawValue) {
            const rawValue = firstBarcode.rawValue;
            if (rawValue.trim().length > 0) {
              const token = rawValue.trim();


              if (this.emitCallback) {
                this.emitCallback(token);
              }
              this.onScanCallback?.(token);

              if (this.autoStopOnScan) {
                await this.stop();
                return;
              }
            }
          }
        } catch {
          // Frame decode error (e.g. invalid frame or temporary decoder issue) - continue next tick
        }
      }

      if (this.isRunning) {
        this.decodeTimer = setTimeout(() => {
          void loop();
        }, this.decodeIntervalMs);
      }
    };

    this.decodeTimer = setTimeout(() => {
      void loop();
    }, this.decodeIntervalMs);
  }

  private resetIdleTimer(): void {
    if (this.idleTimer !== null) {
      clearTimeout(this.idleTimer);
    }
    this.idleTimer = setTimeout(() => {
      if (this.isRunning) {
        this.onIdleTimeoutCallback?.();
        void this.stop();
      }
    }, this.idleTimeoutMs);
  }
}
