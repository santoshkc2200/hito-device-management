/**
 * Forgiving time parser for the paper backfill entry bar (4.6a).
 *
 * Accepts the kinds of shorthand a human types when transcribing a paper
 * register: `0915`, `915`, `9:15`, `9.15am`, `9 15`, `21:05`, `9:05 PM`,
 * `2105`, `115`, `1230am`.
 *
 * Parity-tested against the rules in docs/phases/phase-4/4.6-paper-backfill.md.
 */

export type ParsedTime =
  | { ok: true; date: Date; iso: string; formatted: string }
  | { ok: false; error: string };

/**
 * Parse a human-typed time string and return a Date anchored to `baseDate`.
 *
 * Ambiguity rules (documented):
 * - A 3-digit string like `115` is treated as `01:15` (hours first, 0-padded).
 * - `1230am` → 00:30 (midnight), `1230pm` → 12:30.
 * - Bare numbers with no am/pm indicator use 24-hour interpretation if the
 *   hour-part is ≥ 13, otherwise they remain as-is (not shifted to PM).
 */
export function parseForgivingTime(
  input: string,
  baseDate: Date | string,
): ParsedTime {
  const base = typeof baseDate === "string" ? new Date(baseDate) : baseDate;
  const s = input.trim().toLowerCase().replace(/\s+/g, "");

  if (!s) return { ok: false, error: "Empty input" };

  // Normalise am/pm suffix
  const hasSuffix = s.endsWith("am") || s.endsWith("pm");
  const suffix = hasSuffix ? (s.endsWith("am") ? "am" : "pm") : null;
  const body = hasSuffix ? s.slice(0, -2) : s;

  // Strip all non-digit separators (colon, dot, space already stripped above)
  const digits = body.replace(/[:.]/g, "");

  if (!/^\d+$/.test(digits)) {
    return { ok: false, error: `Cannot parse "${input}" as a time` };
  }

  let hours: number;
  let minutes: number;

  if (digits.length === 1 || digits.length === 2) {
    // "9", "09", "21" → whole hours
    hours = parseInt(digits, 10);
    minutes = 0;
  } else if (digits.length === 3) {
    // "915" → 09:15, "115" → 01:15 (documented: hours first, 0-padded)
    hours = parseInt(digits[0], 10);
    minutes = parseInt(digits.slice(1), 10);
  } else if (digits.length === 4) {
    // "0915", "2105", "1230"
    hours = parseInt(digits.slice(0, 2), 10);
    minutes = parseInt(digits.slice(2), 10);
  } else {
    return { ok: false, error: `Cannot parse "${input}" as a time` };
  }

  if (minutes > 59) {
    return { ok: false, error: `Minutes "${minutes}" out of range` };
  }

  // Apply am/pm
  if (suffix === "am") {
    if (hours === 12) hours = 0; // 12:xx am → 00:xx
  } else if (suffix === "pm") {
    if (hours !== 12) hours += 12; // 1:xx pm → 13:xx, 12:xx pm stays
  }
  // No suffix: keep value as-is (24h assumed when ≥ 13, ambiguous otherwise)

  if (hours > 23) {
    return { ok: false, error: `Hours "${hours}" out of range` };
  }

  // Anchor to base date (local calendar date, preserving TZ offset)
  const result = new Date(base);
  result.setHours(hours, minutes, 0, 0);

  const pad = (n: number) => String(n).padStart(2, "0");
  const formatted = `${pad(hours)}:${pad(minutes)}`;

  // ISO with local offset so the server gets wall-clock intent (FR-74)
  const tzOffsetMs = -result.getTimezoneOffset() * 60_000;
  const tzSign = tzOffsetMs >= 0 ? "+" : "-";
  const tzH = pad(Math.abs(Math.floor(result.getTimezoneOffset() / 60)));
  const tzM = pad(Math.abs(result.getTimezoneOffset() % 60));
  const isoLocal =
    `${result.getFullYear()}-${pad(result.getMonth() + 1)}-${pad(result.getDate())}` +
    `T${formatted}:00${tzSign}${tzH}:${tzM}`;

  return { ok: true, date: result, iso: isoLocal, formatted };
}
