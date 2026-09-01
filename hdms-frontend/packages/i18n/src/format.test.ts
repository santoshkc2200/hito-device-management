import { describe, expect, it } from "vitest";
import { collator, formatDate, formatNumber, formatTime } from "./format";

const iso = "2026-08-31T08:30:00Z"; // 17:30 Asia/Tokyo

describe("format", () => {
  it("formats time in the Tokyo timezone on a 24-hour clock", () => {
    expect(formatTime("ja", iso)).toBe("17:30");
    expect(formatTime("en", iso)).toBe("17:30");
  });

  it("formats a date per locale convention", () => {
    expect(formatDate("en", iso)).toBe("Aug 31");
    expect(formatDate("ja", iso)).toBe("8月31日");
  });

  it("returns an empty string for an unparseable date rather than throwing", () => {
    expect(formatDate("ja", "not-a-date")).toBe("");
    expect(formatTime("en", "")).toBe("");
  });

  it("formats numbers per locale", () => {
    expect(formatNumber("en", 1234)).toBe("1,234");
  });

  it("collates Japanese text in reading order", () => {
    const sorted = ["さとう", "あおき", "たなか"].sort(collator("ja").compare);
    expect(sorted).toEqual(["あおき", "さとう", "たなか"]);
  });
});
