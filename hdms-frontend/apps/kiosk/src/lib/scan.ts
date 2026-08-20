import { useEffect, useMemo } from "react";
import {
  ScanRouter,
  type ScanEvent,
  type ScanSource,
  type Unsubscribe,
} from "@hdms/scan";
import { getKioskConfig, type ScanSourceType } from "./kiosk-config";

/**
 * Placeholder source implementation for uninitialized hardware sources.
 * Real sources (HidWedgeSource, CameraSource, ManualSource) will be plugged in
 * during subphases 3.1b, 3.1c, and 3.4.
 */
class PlaceholderScanSource implements ScanSource {
  readonly id: string;
  readonly label: string;

  constructor(id: string, label: string) {
    this.id = id;
    this.label = label;
  }

  async isAvailable(): Promise<boolean> {
    return false;
  }

  async start(_emit: (rawOrScan: string) => void): Promise<void> {
    // No-op until concrete source is wired in
  }

  async stop(): Promise<void> {
    // No-op
  }
}

function mapConfigTypeToSource(type: ScanSourceType): ScanSource {
  switch (type) {
    case "hid":
      return new PlaceholderScanSource("scanner", "Barcode Scanner (HID)");
    case "camera":
      return new PlaceholderScanSource("camera", "Camera Scanner");
    case "manual":
      return new PlaceholderScanSource("manual", "Attendant Keypad");
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
