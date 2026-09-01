/**
 * The locales HDMS ships. Japanese is the default because the hospital runs
 * in Japanese; English is the second language, not the base one.
 */
export const LOCALES = ["ja", "en"] as const;

export type Locale = (typeof LOCALES)[number];

export const DEFAULT_LOCALE: Locale = "ja";

/** The IANA timezone every date and time in this system is rendered in. */
export const TIMEZONE = "Asia/Tokyo";

export function isLocale(value: unknown): value is Locale {
  return typeof value === "string" && (LOCALES as readonly string[]).includes(value);
}
