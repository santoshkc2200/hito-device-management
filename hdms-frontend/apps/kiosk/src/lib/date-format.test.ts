import { describe, expect, it } from "vitest";
import { dueDateParts, formatHumanDueDate } from "./date-format";

describe("date-format", () => {
  const baseDate = new Date(2026, 7, 31, 9, 0, 0);

  it("returns null for empty or invalid dates", () => {
    expect(dueDateParts(null)).toBeNull();
    expect(dueDateParts("")).toBeNull();
    expect(dueDateParts("invalid-date")).toBeNull();
    expect(formatHumanDueDate(null)).toBeNull();
    expect(formatHumanDueDate("")).toBeNull();
  });

  it("identifies today", () => {
    const today = new Date(2026, 7, 31, 17, 0, 0).toISOString();
    const parts = dueDateParts(today, baseDate);
    expect(parts?.kind).toBe("today");
    const formatted = formatHumanDueDate(today, baseDate, "en");
    expect(formatted).toMatch(/today/i);
  });

  it("identifies tomorrow", () => {
    const tomorrow = new Date(2026, 8, 1, 17, 0, 0).toISOString();
    const parts = dueDateParts(tomorrow, baseDate);
    expect(parts?.kind).toBe("tomorrow");
    const formatted = formatHumanDueDate(tomorrow, baseDate, "en");
    expect(formatted).toMatch(/tomorrow/i);
  });

  it("identifies other future dates", () => {
    const future = new Date(2026, 8, 5, 17, 0, 0).toISOString();
    const parts = dueDateParts(future, baseDate);
    expect(parts?.kind).toBe("other");
    const formatted = formatHumanDueDate(future, baseDate, "en");
    expect(formatted).toMatch(/Sep 5/i);
  });
});

