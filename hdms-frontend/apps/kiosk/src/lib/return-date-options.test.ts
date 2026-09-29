import { describe, expect, it } from "vitest";
import { pickerDays, returnChips, timeSlots } from "./return-date-options";

// All dates are local time: the kiosk shows the iPad's clock.
const local = (y: number, m: number, d: number, h = 0, min = 0) => new Date(y, m - 1, d, h, min);

describe("returnChips", () => {
  it("offers today, tomorrow, +3 and +1 week at 17:00 when unbounded", () => {
    const chips = returnChips(local(2026, 9, 29, 10), null);
    expect(chips.map((c) => c.id)).toEqual(["today", "tomorrow", "plus3", "plus7"]);
    expect(chips[0].at).toEqual(local(2026, 9, 29, 17));
    expect(chips[3].at).toEqual(local(2026, 10, 6, 17));
    expect(chips.every((c) => !c.disabled)).toBe(true);
  });

  it("hides today once 17:00 has passed", () => {
    const chips = returnChips(local(2026, 9, 29, 17, 30), null);
    expect(chips.map((c) => c.id)).toEqual(["tomorrow", "plus3", "plus7"]);
  });

  it("disables chips after the latest return", () => {
    const chips = returnChips(local(2026, 9, 29, 10), local(2026, 9, 30, 9));
    expect(chips.find((c) => c.id === "today")?.disabled).toBe(false);
    expect(chips.find((c) => c.id === "tomorrow")?.disabled).toBe(true);
    expect(chips.find((c) => c.id === "latest")).toBeUndefined();
  });

  it("adds a Latest chip first when every chip is disabled", () => {
    const latest = local(2026, 9, 29, 13);
    const chips = returnChips(local(2026, 9, 29, 12), latest);
    expect(chips[0]).toEqual({ id: "latest", at: latest, disabled: false });
    expect(chips.slice(1).every((c) => c.disabled)).toBe(true);
  });

  it("keeps a chip exactly at the latest return enabled", () => {
    const chips = returnChips(local(2026, 9, 29, 10), local(2026, 9, 29, 17));
    expect(chips.find((c) => c.id === "today")?.disabled).toBe(false);
  });
});

describe("pickerDays", () => {
  it("runs from today to the latest day", () => {
    const days = pickerDays(local(2026, 9, 29, 10), local(2026, 10, 2, 9));
    expect(days).toEqual([local(2026, 9, 29), local(2026, 9, 30), local(2026, 10, 1), local(2026, 10, 2)]);
  });

  it("offers 31 days when unbounded", () => {
    expect(pickerDays(local(2026, 9, 29, 10), null)).toHaveLength(31);
  });
});

describe("timeSlots", () => {
  it("starts at the next quarter hour after now and stops at the latest", () => {
    const slots = timeSlots(local(2026, 9, 29), local(2026, 9, 29, 12, 5), local(2026, 9, 29, 13));
    expect(slots).toEqual([
      local(2026, 9, 29, 12, 15),
      local(2026, 9, 29, 12, 30),
      local(2026, 9, 29, 12, 45),
      local(2026, 9, 29, 13, 0),
    ]);
  });

  it("covers the whole day for a future day", () => {
    const slots = timeSlots(local(2026, 9, 30), local(2026, 9, 29, 12), null);
    expect(slots[0]).toEqual(local(2026, 9, 30, 0, 0));
    expect(slots).toHaveLength(96);
  });
});
