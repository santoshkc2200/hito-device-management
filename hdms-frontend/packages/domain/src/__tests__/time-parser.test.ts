import { describe, expect, it } from "vitest";
import { parseForgivingTime } from "../time-parser";

// Base date: 2026-08-20 (Wednesday), used for anchoring
const BASE = new Date("2026-08-20T00:00:00");

function ok(input: string, expected: string) {
  it(`"${input}" → ${expected}`, () => {
    const result = parseForgivingTime(input, BASE);
    if (!result.ok) throw new Error(`Parse failed: ${result.error}`);
    expect(result.formatted).toBe(expected);
  });
}

function fail(input: string) {
  it(`"${input}" → error`, () => {
    const result = parseForgivingTime(input, BASE);
    expect(result.ok).toBe(false);
  });
}

describe("parseForgivingTime — accepted formats", () => {
  // Colon-separated
  ok("9:15", "09:15");
  ok("09:15", "09:15");
  ok("9:05", "09:05");
  ok("21:05", "21:05");
  ok("0:00", "00:00");
  ok("23:59", "23:59");

  // 4-digit no separator
  ok("0915", "09:15");
  ok("0905", "09:05");
  ok("2105", "21:05");
  ok("1200", "12:00");
  ok("0000", "00:00");

  // 3-digit (hours first)
  ok("915", "09:15");
  ok("905", "09:05");
  ok("115", "01:15"); // documented: NOT 11:05, hours first
  ok("130", "01:30");

  // 2-digit (whole hours)
  ok("9", "09:00");
  ok("09", "09:00");
  ok("21", "21:00");

  // Dot-separator
  ok("9.15", "09:15");
  ok("09.15", "09:15");
  ok("21.05", "21:05");

  // am/pm suffix
  ok("9:15am", "09:15");
  ok("9:15 AM", "09:15");
  ok("9:15pm", "21:15");
  ok("9:15 PM", "21:15");
  ok("9.15am", "09:15");
  ok("12:00am", "00:00"); // midnight
  ok("12:00pm", "12:00"); // noon
  ok("12:30am", "00:30"); // documented: 1230am → 00:30
  ok("1230am", "00:30");
  ok("1230pm", "12:30");

  // With spaces already trimmed
  ok("  09:15  ", "09:15");
});

describe("parseForgivingTime — 24h no-suffix (documented: no shift)", () => {
  ok("1300", "13:00");
  ok("2359", "23:59");
  ok("0001", "00:01");
});

describe("parseForgivingTime — rejected inputs", () => {
  fail("");
  fail("abc");
  fail("9:60"); // minutes out of range
  fail("25:00"); // hours out of range
  fail("99999"); // too many digits
});

describe("parseForgivingTime — ISO output is TZ-aware", () => {
  it("includes a timezone offset rather than Z", () => {
    const result = parseForgivingTime("09:15", BASE);
    if (!result.ok) throw new Error(result.error);
    // ISO must contain a + or - offset, not bare Z
    expect(result.iso).toMatch(/T09:15:00[+-]\d{2}:\d{2}$/);
  });

  it("anchors to the supplied base date", () => {
    const result = parseForgivingTime("09:15", BASE);
    if (!result.ok) throw new Error(result.error);
    expect(result.iso).toContain("2026-08-20");
  });
});
