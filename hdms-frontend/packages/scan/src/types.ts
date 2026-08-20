import type { ParseErrorReason } from "@hdms/domain";

export interface Scan {
  token: string;
  source: string;
  scannedAt: string; // ISO-8601
  raw?: string; // pre-normalisation, for diagnostics only
}

export interface ScanSource {
  readonly id: "scanner" | "camera" | "manual" | "nfc" | string;
  readonly label: string;
  isAvailable(): Promise<boolean>;
  start(emit: (rawOrScan: string | Scan) => void): Promise<void>;
  stop(): Promise<void>;
}

export type ScanEvent =
  | { kind: "scan"; scan: Scan }
  | { kind: "invalid"; raw: string; reason: ParseErrorReason }
  | { kind: "duplicate"; token: string }
  | { kind: "source-state"; id: string; available: boolean };

export type Unsubscribe = () => void;
