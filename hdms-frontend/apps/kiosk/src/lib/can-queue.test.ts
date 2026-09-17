import { describe, expect, it } from "vitest";
import { canQueue, type CanQueueResult } from "./can-queue";
import { CACHE_STALENESS_BOUND_MS } from "./offline-cache";

describe("canQueue policy (Deliverable 5.1b)", () => {
  interface MatrixTestCase {
    description: string;
    action: string;
    cachedState: {
      status?: string;
      isFresh?: boolean;
      ageMs?: number;
    } | null;
    expectedAllowed: boolean;
    expectedReasonCode?: string;
  }

  const matrixCases: MatrixTestCase[] = [
    // --- Return action: ALWAYS queueable regardless of cache state ---
    {
      description: "return with no cache at all",
      action: "return",
      cachedState: null,
      expectedAllowed: true,
    },
    {
      description: "return with fresh available device",
      action: "return",
      cachedState: { status: "available", isFresh: true },
      expectedAllowed: true,
    },
    {
      description: "return with stale available device",
      action: "return",
      cachedState: { status: "available", isFresh: false },
      expectedAllowed: true,
    },
    {
      description: "return with fresh on_loan device",
      action: "return",
      cachedState: { status: "on_loan", isFresh: true },
      expectedAllowed: true,
    },
    {
      description: "return with stale on_loan device",
      action: "return",
      cachedState: { status: "on_loan", isFresh: false },
      expectedAllowed: true,
    },
    {
      description: "return with maintenance device",
      action: "return",
      cachedState: { status: "maintenance", isFresh: true },
      expectedAllowed: true,
    },
    {
      description: "return with lost device",
      action: "return",
      cachedState: { status: "lost", isFresh: false },
      expectedAllowed: true,
    },

    // --- Borrow action: Queueable ONLY when fresh AND available ---
    {
      description: "borrow with no cache at all",
      action: "borrow",
      cachedState: null,
      expectedAllowed: false,
      expectedReasonCode: "no_cache",
    },
    {
      description: "borrow with fresh available device",
      action: "borrow",
      cachedState: { status: "available", isFresh: true },
      expectedAllowed: true,
    },
    {
      description: "borrow with stale available device",
      action: "borrow",
      cachedState: { status: "available", isFresh: false },
      expectedAllowed: false,
      expectedReasonCode: "stale_cache",
    },
    {
      description: "borrow with fresh on_loan device",
      action: "borrow",
      cachedState: { status: "on_loan", isFresh: true },
      expectedAllowed: false,
      expectedReasonCode: "device_unavailable",
    },
    {
      description: "borrow with stale on_loan device",
      action: "borrow",
      cachedState: { status: "on_loan", isFresh: false },
      expectedAllowed: false,
      expectedReasonCode: "stale_cache",
    },
    {
      description: "borrow with fresh maintenance device",
      action: "borrow",
      cachedState: { status: "maintenance", isFresh: true },
      expectedAllowed: false,
      expectedReasonCode: "device_unavailable",
    },
    {
      description: "borrow with stale retired device",
      action: "borrow",
      cachedState: { status: "retired", isFresh: false },
      expectedAllowed: false,
      expectedReasonCode: "stale_cache",
    },
    {
      description: "borrow with fresh lost device",
      action: "borrow",
      cachedState: { status: "lost", isFresh: true },
      expectedAllowed: false,
      expectedReasonCode: "device_unavailable",
    },

    // --- Other actions: Refused ---
    {
      description: "unsupported action (hold_device)",
      action: "hold_device",
      cachedState: { status: "available", isFresh: true },
      expectedAllowed: false,
      expectedReasonCode: "unsupported_action",
    },
  ];

  describe("table-driven classification matrix", () => {
    it.each(matrixCases)(
      "$description -> allowed: $expectedAllowed",
      ({ action, cachedState, expectedAllowed, expectedReasonCode }) => {
        const result: CanQueueResult = canQueue(action, cachedState);

        expect(result.allowed).toBe(expectedAllowed);
        expect(result.canQueue).toBe(expectedAllowed);

        if (expectedAllowed) {
          expect(result).toHaveProperty("action", action);
        } else {
          expect(result).toHaveProperty("reason");
          if (expectedReasonCode) {
            expect(result).toHaveProperty("reasonCode", expectedReasonCode);
          }
        }
      }
    );
  });

  it("borrowWithStaleCacheIsRefusedNotQueued", () => {
    const now = Date.now();
    // Cache timestamp is older than CACHE_STALENESS_BOUND_MS
    const staleTimestamp = now - (CACHE_STALENESS_BOUND_MS + 1000);

    const result = canQueue(
      "borrow",
      { status: "available", cachedAt: staleTimestamp },
      { now }
    );

    expect(result.allowed).toBe(false);
    expect(result.canQueue).toBe(false);
    expect(result).toHaveProperty("reasonCode", "stale_cache");
    expect(result).toHaveProperty("reason");
    if (!result.allowed) {
      expect(result.reason).toBeTruthy();
      expect(result.reason.toLowerCase()).toContain("paper register");
    }
  });

  it("returnIsQueuedEvenWhenCacheIsAbsent", () => {
    const result = canQueue("return", null);

    expect(result.allowed).toBe(true);
    expect(result.canQueue).toBe(true);
    if (result.allowed) {
      expect(result.action).toBe("return");
    }
  });

  it("refusalMentionsThePaperRegister", () => {
    // 1. Borrow refused due to absent cache
    const noCacheResult = canQueue("borrow", null, { locale: "en" });
    expect(noCacheResult.allowed).toBe(false);
    if (!noCacheResult.allowed) {
      expect(noCacheResult.reason.toLowerCase()).toContain("paper register");
      // Must be exactly one sentence (one period at end)
      expect(noCacheResult.reason.trim().endsWith(".")).toBe(true);
      // No HTTP status codes in user-facing copy
      expect(noCacheResult.reason).not.toMatch(/\b(400|401|403|404|408|409|422|500|502|503)\b/);
    }

    // 2. Borrow refused due to stale cache
    const staleResult = canQueue(
      "borrow",
      { status: "available", isFresh: false },
      { locale: "en" }
    );
    expect(staleResult.allowed).toBe(false);
    if (!staleResult.allowed) {
      expect(staleResult.reason.toLowerCase()).toContain("paper register");
      expect(staleResult.reason.trim().endsWith(".")).toBe(true);
      expect(staleResult.reason).not.toMatch(/\b(400|401|403|404|408|409|422|500|502|503)\b/);
    }

    // 3. Borrow refused due to device unavailable
    const unavailableResult = canQueue(
      "borrow",
      { status: "on_loan", isFresh: true },
      { locale: "en" }
    );
    expect(unavailableResult.allowed).toBe(false);
    if (!unavailableResult.allowed) {
      expect(unavailableResult.reason.toLowerCase()).toContain("paper register");
      expect(unavailableResult.reason.trim().endsWith(".")).toBe(true);
      expect(unavailableResult.reason).not.toMatch(/\b(400|401|403|404|408|409|422|500|502|503)\b/);
    }

    // 4. Japanese locale verification
    const jaResult = canQueue("borrow", null, { locale: "ja" });
    expect(jaResult.allowed).toBe(false);
    if (!jaResult.allowed) {
      expect(jaResult.reason).toContain("紙の管理台帳");
      expect(jaResult.reason).not.toMatch(/\b(400|401|403|404|408|409|422|500|502|503)\b/);
    }
  });

  it("borrowWithUndateableCacheIsRefusedNotQueued", () => {
    // A cached object carrying neither isFresh, cachedAt nor ageMs cannot be
    // dated, so its freshness is unverifiable — the borrow must fail closed.
    const result = canQueue("borrow", { status: "available" }, { locale: "en" });

    expect(result.allowed).toBe(false);
    if (!result.allowed) {
      expect(result.reasonCode).toBe("stale_cache");
      expect(result.reason.toLowerCase()).toContain("paper register");
    }
  });

  it("returnIsStillQueuedWithAnUndateableCache", () => {
    const result = canQueue("return", { status: "on_loan" });
    expect(result.allowed).toBe(true);
  });
});
