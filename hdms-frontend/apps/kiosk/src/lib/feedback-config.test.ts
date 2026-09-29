import { describe, expect, it } from "vitest";
import { outcomeSoundKey } from "./feedback-config";

describe("outcomeSoundKey", () => {
  it("stays the same when only the loan's due date changes", () => {
    const borrowed = { kind: "borrowed" as const, loanId: "loan-1", dueAt: "2026-09-30T08:00:00Z" };
    expect(outcomeSoundKey({ ...borrowed, dueAt: "2026-09-30T06:00:00Z" })).toBe(outcomeSoundKey(borrowed));
  });

  it("differs for a different loan", () => {
    expect(outcomeSoundKey({ kind: "borrowed", loanId: "loan-2" })).not.toBe(outcomeSoundKey({ kind: "borrowed", loanId: "loan-1" }));
  });
});
