const UNITS = ["B", "KiB", "MiB", "GiB", "TiB"];

export function formatBytes(n: number): string {
  if (n <= 0) return "0 B";
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), UNITS.length - 1);
  if (i === 0) return `${n} B`;
  return `${(n / 1024 ** i).toFixed(1)} ${UNITS[i]}`;
}

export function formatDateTime(iso?: string | null, locale?: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleString(locale, { year: "numeric", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

export const WORKER_STALE_MS = 3 * 60_000;
export const BACKUP_STALE_MS = 26 * 60 * 60_000;
