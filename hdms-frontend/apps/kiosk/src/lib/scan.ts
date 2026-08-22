import { useCallback, useEffect, useMemo } from "react";
import {
  ScanRouter,
  HidWedgeSource,
  ManualSource,
  type ScanEvent,
  type ScanSource,
  type Unsubscribe,
} from "@hdms/scan";
import { getKioskConfig, type ScanSourceType } from "./kiosk-config";

function mapConfigTypeToSource(type: ScanSourceType): ScanSource | null {
  switch (type) {
    case "hid":
      return new HidWedgeSource();
    case "camera":
      // CameraSource starts from CameraOverlay only. Registering it here
      // would call getUserMedia during kiosk startup.
      return null;
    case "manual":
      return new ManualSource();
  }
}


export interface UseScanRouterResult {
  subscribe: (listener: (event: ScanEvent) => void) => Unsubscribe;
  getRouter: () => ScanRouter;
  start: () => Promise<void>;
  stop: () => Promise<void>;
}

/**
 * React hook that manages the lifecycle of ScanRouter for the kiosk app.
 * This hook is the sole integration point between apps/kiosk and @hdms/scan.
 */
export function useScanRouter(): UseScanRouterResult {
  const router = useMemo(() => {
    const r = new ScanRouter();
    const config = getKioskConfig();
    const enabledSources = config?.enabledSources ?? ["hid", "camera", "manual"];

    for (const sourceType of enabledSources) {
      const source = mapConfigTypeToSource(sourceType);
      if (source) r.register(source);
    }
    return r;
  }, []);

  useEffect(() => {
    return () => {
      void router.stop();
    };
  }, [router]);

  const start = useCallback(() => router.start(), [router]);
  const stop = useCallback(() => router.stop(), [router]);

  return {
    subscribe: (listener) => router.subscribe(listener),
    getRouter: () => router,
    start,
    stop,
  };
}
