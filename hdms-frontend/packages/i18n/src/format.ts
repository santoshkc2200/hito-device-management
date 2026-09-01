import { TIMEZONE, type Locale } from "./locale";

function parse(iso: string | null | undefined): Date | null {
  if (!iso || typeof iso !== "string" || iso.trim() === "") return null;
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? null : date;
}

/** Month and day, per locale convention. Gregorian — no Japanese era. */
export function formatDate(locale: Locale, iso: string | null | undefined): string {
  const date = parse(iso);
  if (!date) return "";
  return new Intl.DateTimeFormat(locale, {
    month: "short",
    day: "numeric",
    timeZone: TIMEZONE,
  }).format(date);
}

/** 24-hour clock in both locales — a counter is not the place for am/pm. */
export function formatTime(locale: Locale, iso: string | null | undefined): string {
  const date = parse(iso);
  if (!date) return "";
  return new Intl.DateTimeFormat(locale, {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
    timeZone: TIMEZONE,
  }).format(date);
}

export function formatNumber(locale: Locale, value: number): string {
  return new Intl.NumberFormat(locale).format(value);
}

export function formatList(locale: Locale, items: string[]): string {
  return new Intl.ListFormat(locale, { style: "short", type: "conjunction" }).format(items);
}

const collators = new Map<Locale, Intl.Collator>();

/** Sorting Japanese names needs a collator; `<` gives codepoint order. */
export function collator(locale: Locale): Intl.Collator {
  const existing = collators.get(locale);
  if (existing) return existing;
  const created = new Intl.Collator(locale, { sensitivity: "base", numeric: true });
  collators.set(locale, created);
  return created;
}
