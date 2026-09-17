import { beforeEach, describe, expect, it } from "vitest";
import {
  cacheDevice,
  getCachedDevice,
  cacheUser,
  getCachedUser,
  clearOfflineCache,
  CACHE_STALENESS_BOUND_MS,
} from "./offline-cache";

describe("offline-cache (Deliverable 5.1b)", () => {
  beforeEach(() => {
    clearOfflineCache();
  });

  it("caches and retrieves device by id and assetTag", () => {
    const device = {
      id: "dev-uuid-001",
      assetTag: "HD-D-5500",
      name: "Ultrasound Probe",
      status: "available",
      condition: "good",
    };

    cacheDevice(device);

    const byId = getCachedDevice("dev-uuid-001");
    expect(byId).not.toBeNull();
    expect(byId?.data.name).toBe("Ultrasound Probe");
    expect(byId?.isFresh).toBe(true);
    expect(byId?.isStale).toBe(false);
    expect(byId?.isLastKnown).toBe(true);

    const byTag = getCachedDevice("HD-D-5500");
    expect(byTag).not.toBeNull();
    expect(byTag?.data.id).toBe("dev-uuid-001");
  });

  it("exposes entry age and marks stale when older than staleness bound", () => {
    const cachedAt = 1000000;
    const device = {
      id: "dev-uuid-002",
      assetTag: "HD-D-5501",
      name: "Infusion Pump",
      status: "available",
    };

    cacheDevice(device, cachedAt);

    // Fresh lookup: 1 minute later
    const freshNow = cachedAt + 60 * 1000;
    const freshEntry = getCachedDevice("dev-uuid-002", freshNow);
    expect(freshEntry?.ageMs).toBe(60000);
    expect(freshEntry?.isFresh).toBe(true);
    expect(freshEntry?.isStale).toBe(false);
    expect(freshEntry?.isLastKnown).toBe(true);

    // Stale lookup: CACHE_STALENESS_BOUND_MS + 1ms later
    const staleNow = cachedAt + CACHE_STALENESS_BOUND_MS + 1;
    const staleEntry = getCachedDevice("dev-uuid-002", staleNow);
    expect(staleEntry?.ageMs).toBe(CACHE_STALENESS_BOUND_MS + 1);
    expect(staleEntry?.isFresh).toBe(false);
    expect(staleEntry?.isStale).toBe(true);
    expect(staleEntry?.isLastKnown).toBe(true);
  });

  it("caches and retrieves user by id and employeeNo", () => {
    const user = {
      id: "user-uuid-123",
      fullName: "Dr. Taro Yamada",
      department: "Cardiology",
      employeeNo: "EMP-9001",
    };

    cacheUser(user);

    const byId = getCachedUser("user-uuid-123");
    expect(byId).not.toBeNull();
    expect(byId?.data.fullName).toBe("Dr. Taro Yamada");
    expect(byId?.isLastKnown).toBe(true);

    const byEmp = getCachedUser("EMP-9001");
    expect(byEmp).not.toBeNull();
    expect(byEmp?.data.id).toBe("user-uuid-123");
  });

  it("returns null for uncached items", () => {
    expect(getCachedDevice("non-existent")).toBeNull();
    expect(getCachedUser("non-existent")).toBeNull();
  });
});
