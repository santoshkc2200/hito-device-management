import { STEPS, type RestoreView, type Snapshot, type Source, type Status } from "@/lib/api";

export const localSource: Source = { id: "local", kind: "local", folder: "/var/backups/hdms", hasRecoveryKey: true };
export const nasSource: Source = {
  id: "path:/drives/WardNAS/hdms-backups",
  kind: "destination",
  name: "Ward NAS",
  folder: "/drives/WardNAS/hdms-backups",
  hasRecoveryKey: true,
};
export const usbSource: Source = { id: "path:/drives/usb/hdms", kind: "folder", folder: "/drives/usb/hdms", hasRecoveryKey: true };
export const localWithoutKey: Source = { ...localSource, hasRecoveryKey: false };

export const workingStatus: Status = { database: "working", worker: "ready", restoreRunning: false };

// 2026-09-29T17:00Z is Wednesday 30 Sep, 02:00 in Tokyo.
export const snapshots: Snapshot[] = [
  { id: "snap-new", takenAt: "2026-09-29T17:00:00Z", sizeBytes: 1_500_000 },
  { id: "snap-old", takenAt: "2026-09-28T17:00:00Z", sizeBytes: 1_400_000 },
];

export function view(overrides: Partial<RestoreView> = {}): RestoreView {
  return {
    kind: "restore",
    phase: "running",
    step: "restore_scratch",
    steps: STEPS,
    sourceKind: "local",
    snapshotTakenAt: "2026-09-29T17:00:00Z",
    startedAt: "2026-10-01T00:00:00Z",
    canUndo: false,
    ...overrides,
  };
}
