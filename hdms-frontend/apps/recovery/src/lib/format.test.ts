import { describe, expect, it } from "vitest";
import { formatAgo, formatBytes, formatSnapshotDate } from "./format";

describe("format", () => {
  it("formatBytes uses decimal units like a disk label", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(999)).toBe("999 B");
    expect(formatBytes(1_500_000)).toBe("1.5 MB");
    expect(formatBytes(2_000_000_000)).toBe("2.0 GB");
  });

  it("formatSnapshotDate shows hospital time with the weekday", () => {
    const en = formatSnapshotDate("en", "2026-09-29T17:00:00Z");
    expect(en).toContain("Wednesday");
    expect(en).toContain("Sep 30");
    expect(en).toContain("02:00");
    expect(formatSnapshotDate("ja", "2026-09-29T17:00:00Z")).toContain("水曜日");
  });

  it("formatAgo counts back in minutes, hours, then days", () => {
    const now = new Date("2026-09-30T17:00:00Z");
    expect(formatAgo("en", "2026-09-30T16:59:50Z", now)).toBe("1 minute ago");
    expect(formatAgo("en", "2026-09-30T14:00:00Z", now)).toBe("3 hours ago");
    expect(formatAgo("en", "2026-09-29T17:00:00Z", now)).toBe("1 day ago");
    expect(formatAgo("ja", "2026-09-29T17:00:00Z", now)).toMatch(/1\s?日前/);
  });
});
