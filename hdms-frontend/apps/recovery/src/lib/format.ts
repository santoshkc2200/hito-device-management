import { TIMEZONE, type Locale } from "@hdms/i18n";

const UNITS = ["B", "KB", "MB", "GB", "TB"];

/** Decimal units: people compare this with what a disk's label says. */
export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return "0 B";
  const i = Math.min(Math.floor(Math.log10(n) / 3), UNITS.length - 1);
  if (i === 0) return `${n} B`;
  return `${(n / 1000 ** i).toFixed(1)} ${UNITS[i]}`;
}

/** "Wednesday, Sep 30, 02:00" — hospital time, whatever the browser's zone. */
export function formatSnapshotDate(locale: Locale, iso: string): string {
  return new Intl.DateTimeFormat(locale, {
    weekday: "long",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
    timeZone: TIMEZONE,
  }).format(new Date(iso));
}

/** "1 day ago". Snapshots are always in the past, so the floor is one minute. */
export function formatAgo(locale: Locale, iso: string, now: Date): string {
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "always" });
  const minutes = Math.max(1, Math.round((now.getTime() - new Date(iso).getTime()) / 60_000));
  if (minutes < 60) return rtf.format(-minutes, "minute");
  const hours = Math.round(minutes / 60);
  if (hours < 24) return rtf.format(-hours, "hour");
  return rtf.format(-Math.round(hours / 24), "day");
}
