import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import {
  connectivityManager,
  isKioskOffline,
  recordRequestFailure,
  recordRequestSuccess,
  resetConnectivityForTesting,
} from "./connectivity";

describe("Connectivity Detection and Background Probe", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    resetConnectivityForTesting(false);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("twoConsecutiveFailuresGoOffline, oneSuccessComesBackOnline", () => {
    expect(isKioskOffline()).toBe(false);

    // 1st failure: still online
    recordRequestFailure();
    expect(isKioskOffline()).toBe(false);
    expect(connectivityManager.getState().consecutiveFailures).toBe(1);

    // 2nd failure: flips to offline
    recordRequestFailure();
    expect(isKioskOffline()).toBe(true);
    expect(connectivityManager.getState().consecutiveFailures).toBe(2);

    // 1 single success brings kiosk back online
    recordRequestSuccess();
    expect(isKioskOffline()).toBe(false);
    expect(connectivityManager.getState().consecutiveFailures).toBe(0);
  });

  it("onlineEventAloneDoesNotClearOfflineState", async () => {
    // Put kiosk in offline state
    recordRequestFailure();
    recordRequestFailure();
    expect(isKioskOffline()).toBe(true);

    let probeCalled = false;
    connectivityManager.setProbeFn(async () => {
      probeCalled = true;
      // Gateway is still unreachable
      return false;
    });

    // Window receives 'online' event (e.g. Wi-Fi reassociated but no internet gateway)
    connectivityManager.handleOnlineEvent();

    // Advance timers so probe runs
    await vi.advanceTimersByTimeAsync(50);

    expect(probeCalled).toBe(true);
    // MUST remain offline because probe returned false
    expect(isKioskOffline()).toBe(true);
  });

  it("backgroundProbeWithBackoffRecoversWhenProbeSucceeds", async () => {
    let probeAttempts = 0;
    connectivityManager.setProbeFn(async () => {
      probeAttempts += 1;
      // Succeed on 3rd probe
      return probeAttempts >= 3;
    });

    const reconnectSpy = vi.fn();
    const unsubscribe = connectivityManager.onReconnect(reconnectSpy);

    // Go offline
    recordRequestFailure();
    recordRequestFailure();
    expect(isKioskOffline()).toBe(true);

    // 1st probe fires at 1s (probeAttempts = 1, fails)
    await vi.advanceTimersByTimeAsync(1000);
    expect(probeAttempts).toBe(1);
    expect(isKioskOffline()).toBe(true);

    // 2nd probe fires at 2s (probeAttempts = 2, fails)
    await vi.advanceTimersByTimeAsync(2000);
    expect(probeAttempts).toBe(2);
    expect(isKioskOffline()).toBe(true);

    // 3rd probe fires at 4s (probeAttempts = 3, succeeds!)
    await vi.advanceTimersByTimeAsync(4000);
    expect(probeAttempts).toBe(3);
    expect(isKioskOffline()).toBe(false);
    expect(reconnectSpy).toHaveBeenCalledTimes(1);

    unsubscribe();
  });
});
