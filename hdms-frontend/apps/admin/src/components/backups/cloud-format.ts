import type { BackupCloudAccount, BackupConfig, BackupDestination } from "@hdms/api-client";
import { realLocation } from "./real-location";

const SEGMENT = /^[\p{L}\p{N}_-][\p{L}\p{N} ._-]*$/u;

/** Mirrors the server's CleanCloudFolder; the server stays the authority. */
export function isCloudFolder(raw: string): boolean {
  const s = raw.trim().replace(/^\/+|\/+$/g, "");
  if (s.length === 0 || s.length > 200) return false;
  const parts = s.split("/");
  return parts.length <= 3 && parts.every((p) => SEGMENT.test(p) && p === p.trim());
}

/** Where a destination keeps its copy, in words an administrator recognises. */
export function destinationLocation(
  d: BackupDestination,
  config: Pick<BackupConfig, "drivesDir" | "drivesHostPath"> | undefined,
  providerLabel: (provider: string) => string,
): string {
  if (d.cloudAccountId && d.folder) return `${providerLabel(d.provider)} · ${d.folder}`;
  return realLocation(d.target, config);
}

export function accountLabel(a: BackupCloudAccount, providerLabel: (provider: string) => string): string {
  return a.accountEmail ? `${providerLabel(a.provider)} · ${a.accountEmail}` : providerLabel(a.provider);
}
