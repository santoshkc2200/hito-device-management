import * as React from "react";

let wakeLockSentinel: any = null;

export async function requestScreenWakeLock(): Promise<boolean> {
  if (typeof navigator === "undefined" || !("wakeLock" in navigator)) {
    return false;
  }
  try {
    wakeLockSentinel = await (navigator as any).wakeLock.request("screen");
    wakeLockSentinel.addEventListener("release", () => {
      wakeLockSentinel = null;
    });
    return true;
  } catch {
    wakeLockSentinel = null;
    return false;
  }
}

export async function releaseScreenWakeLock(): Promise<void> {
  try {
    if (wakeLockSentinel) {
      await wakeLockSentinel.release();
      wakeLockSentinel = null;
    }
  } catch {
    wakeLockSentinel = null;
  }
}

export function useScreenWakeLock(enabled: boolean = true) {
  React.useEffect(() => {
    if (!enabled) return;

    void requestScreenWakeLock();

    const handleVisibilityChange = () => {
      if (document.visibilityState === "visible") {
        void requestScreenWakeLock();
      }
    };

    document.addEventListener("visibilitychange", handleVisibilityChange);

    return () => {
      document.removeEventListener("visibilitychange", handleVisibilityChange);
      void releaseScreenWakeLock();
    };
  }, [enabled]);
}
