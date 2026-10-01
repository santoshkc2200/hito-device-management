// The worker's recovery API (/recovery/api/*). It is not in the OpenAPI
// contract — the API process is not involved — so the shapes live here and
// must match hdms-backend/internal/platform/recovery/handler.go and state.go.

export type LiveState = "working" | "empty" | "damaged" | "server_down";
export type WorkerMode = "starting" | "database_unavailable" | "ready";
export type SourceKind = "local" | "folder" | "destination";
export type Step =
  | "safety_backup"
  | "restore_scratch"
  | "migrate_scratch"
  | "validate"
  | "maintenance_on"
  | "copy_forward"
  | "swap"
  | "maintenance_off"
  | "record";

/** Every step a full restore runs, in order (engine.go `plan`). */
export const STEPS: Step[] = [
  "safety_backup",
  "restore_scratch",
  "migrate_scratch",
  "validate",
  "maintenance_on",
  "copy_forward",
  "swap",
  "maintenance_off",
  "record",
];

/** What the worker requires typed before a restore or an undo. */
export const CONFIRM_WORD = "RESTORE";

export interface Status {
  database: LiveState;
  worker: WorkerMode;
  restoreRunning: boolean;
}

export interface Source {
  id: string;
  kind: SourceKind;
  name?: string;
  folder: string;
  hasRecoveryKey: boolean;
}

export interface Snapshot {
  id: string;
  takenAt: string;
  sizeBytes: number;
}

export interface RestoreView {
  kind: "restore" | "undo";
  phase: "running" | "completed" | "failed";
  step: Step;
  steps: Step[];
  sourceKind: SourceKind;
  sourceName?: string;
  snapshotTakenAt: string;
  startedAt: string;
  finishedAt?: string;
  error?: string;
  warning?: string;
  canUndo: boolean;
}

export class RecoveryError extends Error {
  readonly status: number;
  readonly code: string;
  readonly retryAfterSeconds?: number;

  constructor(status: number, code: string, retryAfterSeconds?: number) {
    super(code);
    this.name = "RecoveryError";
    this.status = status;
    this.code = code;
    this.retryAfterSeconds = retryAfterSeconds;
  }

  /** The worker no longer knows this browser: 30 minutes idle, or it restarted. */
  get sessionLost(): boolean {
    return this.status === 401 && this.code === "session_required";
  }
}

export function asRecoveryError(err: unknown): RecoveryError {
  return err instanceof RecoveryError ? err : new RecoveryError(0, "unexpected");
}

const API_BASE = "/recovery/api";

async function call<T>(method: "GET" | "POST", path: string, body?: unknown): Promise<T> {
  let res: Response;
  try {
    res = await fetch(`${API_BASE}${path}`, {
      method,
      credentials: "same-origin",
      headers: body === undefined ? undefined : { "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new RecoveryError(0, "unreachable");
  }
  const payload = (await res.json().catch(() => ({}))) as Record<string, unknown>;
  if (!res.ok) {
    const code = typeof payload.error === "string" ? payload.error : "unexpected";
    const retry = Number(res.headers.get("Retry-After"));
    throw new RecoveryError(res.status, code, Number.isFinite(retry) && retry > 0 ? retry : undefined);
  }
  return payload as T;
}

export const recoveryApi = {
  status: () => call<Status>("GET", "/status"),
  sources: () => call<{ sources: Source[] }>("GET", "/sources").then((r) => r.sources),
  unlock: (source: string, key: string) => call<{ keysMatch: true }>("POST", "/unlock", { source, key }),
  snapshots: () => call<{ snapshots: Snapshot[] }>("GET", "/snapshots").then((r) => r.snapshots),
  restore: () => call<{ restore: RestoreView | null }>("GET", "/restore").then((r) => r.restore),
  startRestore: (snapshotId: string) =>
    call<{ restore: RestoreView }>("POST", "/restore", { snapshotId, confirmation: CONFIRM_WORD }).then((r) => r.restore),
  undo: () => call<{ restore: RestoreView }>("POST", "/restore/undo", { confirmation: CONFIRM_WORD }).then((r) => r.restore),
};
