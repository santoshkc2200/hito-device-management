import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  clearKioskConfig,
  clearSessionId,
  CURRENT_SCHEMA_VERSION,
  getKioskConfig,
  getSessionId,
  KIOSK_CONFIG_STORAGE_KEY,
  setKioskConfig,
  setSessionId,
} from "./kiosk-config";

describe("kiosk-config", () => {
  beforeEach(() => {
    localStorage.clear();
    sessionStorage.clear();
  });

  it("returns null when no config is stored", () => {
    expect(getKioskConfig()).toBeNull();
  });

  it("valid config round-trips correctly", () => {
    const saved = setKioskConfig({
      kioskId: "kiosk-123",
      kioskName: "ICU Entrance Kiosk",
      token: "secret-token-abc",
      muteEnabled: true,
      enabledSources: ["hid", "camera"],
    });

    expect(saved).toEqual({
      schemaVersion: CURRENT_SCHEMA_VERSION,
      kioskId: "kiosk-123",
      kioskName: "ICU Entrance Kiosk",
      token: "secret-token-abc",
      muteEnabled: true,
      enabledSources: ["hid", "camera"],
      attendantPinHash: undefined,
      defaultLocale: "ja",
    });

    const loaded = getKioskConfig();
    expect(loaded).toEqual(saved);
  });

  it("stale schemaVersion is discarded and returns null", () => {
    localStorage.setItem(
      KIOSK_CONFIG_STORAGE_KEY,
      JSON.stringify({
        schemaVersion: 0,
        kioskId: "kiosk-old",
        kioskName: "Old Kiosk",
        token: "token-old",
      })
    );

    expect(getKioskConfig()).toBeNull();
    expect(localStorage.getItem(KIOSK_CONFIG_STORAGE_KEY)).toBeNull();
  });

  it("corrupted JSON is discarded gracefully without throwing", () => {
    localStorage.setItem(KIOSK_CONFIG_STORAGE_KEY, "invalid-json{{");

    expect(getKioskConfig()).toBeNull();
    expect(localStorage.getItem(KIOSK_CONFIG_STORAGE_KEY)).toBeNull();
  });

  it("clearKioskConfig wipes the stored config immediately", () => {
    setKioskConfig({
      kioskId: "kiosk-123",
      kioskName: "ICU Entrance Kiosk",
      token: "secret-token-abc",
    });

    const spy = vi.spyOn(Storage.prototype, "removeItem");
    clearKioskConfig();

    expect(spy).toHaveBeenCalledWith(KIOSK_CONFIG_STORAGE_KEY);
    expect(getKioskConfig()).toBeNull();
    spy.mockRestore();
  });

  it("session ID can be set, retrieved, and cleared", () => {
    expect(getSessionId()).toBeNull();
    setSessionId("session-xyz-456");
    expect(getSessionId()).toBe("session-xyz-456");
    clearSessionId();
    expect(getSessionId()).toBeNull();
  });
});

describe("locale in the kiosk config", () => {
  beforeEach(() => localStorage.clear());

  it("stores the locale the kiosk was paired with", () => {
    setKioskConfig({
      kioskId: "k1",
      kioskName: "Ward 3",
      token: "tok",
      defaultLocale: "en",
    });
    expect(getKioskConfig()?.defaultLocale).toBe("en");
  });

  it("defaults to Japanese when the stored config predates the field", () => {
    setKioskConfig({ kioskId: "k1", kioskName: "Ward 3", token: "tok" });
    expect(getKioskConfig()?.defaultLocale).toBe("ja");
  });

  it("discards a config written by the previous schema version", () => {
    localStorage.setItem(
      KIOSK_CONFIG_STORAGE_KEY,
      JSON.stringify({ schemaVersion: 1, kioskId: "k1", kioskName: "W", token: "t" })
    );
    expect(getKioskConfig()).toBeNull();
  });

  it("ignores a stored locale that is not a supported one", () => {
    localStorage.setItem(
      KIOSK_CONFIG_STORAGE_KEY,
      JSON.stringify({
        schemaVersion: 2,
        kioskId: "k1",
        kioskName: "W",
        token: "t",
        defaultLocale: "de",
      })
    );
    expect(getKioskConfig()?.defaultLocale).toBe("ja");
  });
});

