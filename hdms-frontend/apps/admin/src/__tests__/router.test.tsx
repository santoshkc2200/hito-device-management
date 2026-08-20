import { describe, expect, it } from "vitest";
import { router } from "../router";

describe("Router completeness", () => {
  it("registers all expected Phase 4 routes", () => {
    const routes = router.routesByPath;
    const paths = Object.keys(routes);

    const expectedPaths = [
      "/login",
      "/",
      "/dashboard",
      "/backfill",
      "/devices",
      "/devices/$deviceId",
      "/users",
      "/users/$userId",
      "/register",
      "/loans",
      "/loans/$loanId",
      "/disputed",
      "/reports",
      "/audit",
      "/settings",
      "/credentials",
      "/labels",
      "/card-reader-test",
    ];

    for (const expected of expectedPaths) {
      expect(paths).toContain(expected);
      expect(routes[expected as keyof typeof routes]).toBeDefined();
    }
  });
});
