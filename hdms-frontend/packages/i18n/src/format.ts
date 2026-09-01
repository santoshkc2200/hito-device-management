import { TIMEZONE, type Locale } from "./locale";

function parse(input: Date | string | null | undefined): Date | null {
  if (!input) return null;
  if (input instanceof Date) return Number.isNaN(input.getTime()) ? null : input;
  if (typeof input !== "string" || input.trim() === "") return null;
  const date = new Date(input);
  return Number.isNaN(date.getTime()) ? null : date;
}

/** Month and day, per locale convention. Gregorian — no Japanese era. */
export function formatDate(locale: Locale, input: Date | string | null | undefined): string {
  const date = parse(input);
  if (!date) return "";
  return new Intl.DateTimeFormat(locale, {
    month: "short",
    day: "numeric",
    timeZone: TIMEZONE,
  }).format(date);
}

/** 24-hour clock in both locales — a counter is not the place for am/pm. */
export function formatTime(locale: Locale, input: Date | string | null | undefined): string {
  const date = parse(input);
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
