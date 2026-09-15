export { ScanRouter } from "./router";
export type { ScanRouterOptions } from "./router";
export { FakeSource } from "./fake-source";
export {
  HidWedgeSource,
  HidWedgeInterpreter,
  MAX_INTERVAL_MS,
  MIN_LENGTH,
  IDLE_RESET_MS,
  MAX_BUFFER_LENGTH,
  DEFAULT_FRESHNESS_WINDOW_MS,
  MAX_DIAGNOSTICS_ENTRIES,
} from "./hid-wedge";
export type {
  HidWedgeOptions,
  KeyInputEvent,
  ProcessKeyResult,
  RawSequenceDiagnostic,
} from "./hid-wedge";
export {
  CameraSource,
  CAMERA_SUPPORTED_FORMATS,
  DEFAULT_DECODE_INTERVAL_MS,
  DEFAULT_CAMERA_IDLE_TIMEOUT_MS,
} from "./camera";
export type {
  CameraFacingMode,
  CameraSourceOptions,
  SupportedBarcodeFormat,
} from "./camera";
export { ManualSource } from "./manual";
export type { ManualSourceOptions } from "./manual";
export type {
  Scan,
  ScanSource,
  ScanEvent,
  Unsubscribe,
} from "./types";


