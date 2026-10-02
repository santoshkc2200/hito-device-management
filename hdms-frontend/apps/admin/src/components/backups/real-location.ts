import type { BackupConfig } from "@hdms/api-client";

// realLocation turns a worker path under the drives folder (/drives/...) into
// where it really is on the server (/Volumes/..., /mnt/...). Without a known
// host location the worker path is the best answer, so it is returned as is.
export function realLocation(path: string, config?: Pick<BackupConfig, "drivesDir" | "drivesHostPath">): string {
  const dir = config?.drivesDir;
  const host = config?.drivesHostPath?.replace(/[/\\]+$/, "");
  if (!dir || !host) return path;
  if (path === dir) return host;
  if (path.startsWith(`${dir}/`)) return host + path.slice(dir.length);
  return path;
}
