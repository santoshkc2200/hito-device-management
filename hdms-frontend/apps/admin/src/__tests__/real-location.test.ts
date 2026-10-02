import { describe, expect, it } from "vitest";
import { realLocation } from "@/components/backups/real-location";

describe("realLocation", () => {
  const cfg = { drivesDir: "/drives", drivesHostPath: "/Volumes/" };

  it("swaps the drives folder for its host location", () => {
    expect(realLocation("/drives/BackupSSD/hdms", cfg)).toBe("/Volumes/BackupSSD/hdms");
    expect(realLocation("/drives", cfg)).toBe("/Volumes");
  });

  it("leaves other paths alone, including look-alike prefixes", () => {
    expect(realLocation("/var/backups/hdms/repo", cfg)).toBe("/var/backups/hdms/repo");
    expect(realLocation("/drivesX/a", cfg)).toBe("/drivesX/a");
  });

  it("falls back to the path when the host location is unknown", () => {
    expect(realLocation("/drives/usb", { drivesDir: "/drives" })).toBe("/drives/usb");
    expect(realLocation("/drives/usb", undefined)).toBe("/drives/usb");
    expect(realLocation("/drives/usb", { drivesDir: "", drivesHostPath: "/mnt" })).toBe("/drives/usb");
  });
});
