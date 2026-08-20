import { useEffect, useMemo } from "react";
import {
  ScanRouter,
  HidWedgeSource,
  CameraSource,
  ManualSource,
  type ScanEvent,
  type ScanSource,
  type Unsubscribe,
} from "@hdms/scan";
import { getKioskConfig, type ScanSourceType } from "./kiosk-config";

function mapConfigTypeToSource(type: ScanSourceType): ScanSource {
  switch (type) {
    case "hid":
      return new HidWedgeSource();
    case "camera":
      return new CameraSource();
    case "manual":
      return new ManualSource();
  }
}


export interface UseScanRouterResult {
  subscribe: (listener: (event: ScanEvent) => void) => Unsubscribe;
  getRouter: () => ScanRouter;
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
      r.register(mapConfigTypeToSource(sourceType));
    }
    return r;
  }, []);

  useEffect(() => {
    void router.start();

    return () => {
      void router.stop();
    };
  }, [router]);

  return {
    subscribe: (listener) => router.subscribe(listener),
    getRouter: () => router,
  };
}
