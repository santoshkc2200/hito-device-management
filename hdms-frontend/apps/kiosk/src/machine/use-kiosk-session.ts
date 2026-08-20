import * as React from "react";
import { useSyncExternalStore } from "react";
import { createActor } from "xstate";
import type { ScanSource } from "@hdms/api-client";
import type { SessionState } from "@hdms/domain";
import {
  sessionMachine,
  executeScan,
  executeReturnLoan,
  executeClose,
  executeCancel,
  executeResume,
  setupVisibilityReconciliation,
  type SessionMachineActor,
} from "./session-machine";
import { getKioskConfig, getSessionId } from "../lib/kiosk-config";
import { parseProblem } from "../lib/problem";
import { useScanRouter } from "../lib/scan";

export interface UseKioskSessionOptions {
  actor?: SessionMachineActor;
}

export function useKioskSession(options?: UseKioskSessionOptions) {
  const config = getKioskConfig();
  const kioskName = config?.kioskName ?? "HDMS Kiosk";
  const kioskId = config?.kioskId ?? "unpaired-kiosk";

  const { subscribe: subscribeScan, getRouter } = useScanRouter();

  // Create or reuse actor
  const actor = React.useMemo(() => {
    if (options?.actor) return options.actor;
    const act = createActor(sessionMachine);
    act.start();
    return act;
  }, [options?.actor]);

  const snapshot = useSyncExternalStore(
    (onStoreChange) => {
      const sub = actor.subscribe(() => onStoreChange());
      return () => sub.unsubscribe();
    },
    () => actor.getSnapshot()
  );

  const state = (typeof snapshot.value === "string" ? snapshot.value : "idle") as SessionState;

  const [isCameraOpen, setIsCameraOpen] = React.useState(false);
  const [isDiagnosticsOpen, setIsDiagnosticsOpen] = React.useState(false);
  const [scannerFresh, setScannerFresh] = React.useState(true);
  const [scannerReady, setScannerReady] = React.useState(true);

  // Resume stored session on mount if one exists
  React.useEffect(() => {
    const storedSessionId = getSessionId();
    if (storedSessionId && state === "idle") {
      void executeResume({ sessionId: storedSessionId }).then((session) => {
        if (session) {
          actor.send({ type: "RESTORE_SESSION", session });
        }
      });
    }
  }, [actor, state]);

  // Foreground visibility reconciliation
  React.useEffect(() => {
    return setupVisibilityReconciliation(actor);
  }, [actor]);

  // Handle hardware scan and camera scan routing
  const handleScan = React.useCallback(
    async (token: string, source: ScanSource = "scanner") => {
      try {
        const result = await executeScan({
          sessionId: snapshot.context.sessionId,
          kioskId,
          token,
          source,
        });
        actor.send({ type: "APPLY_SCAN_RESULT", result });
      } catch (err) {
        const problem = await parseProblem(err);
        actor.send({ type: "SET_PROBLEM", problem });
      }
    },
    [actor, kioskId, snapshot.context.sessionId]
  );

  // Subscribe to ScanRouter events
  React.useEffect(() => {
    const unsubscribe = subscribeScan((event) => {
      if (event.kind === "scan") {
        const scanSource: ScanSource =
          event.scan.source === "camera"
            ? "camera"
            : event.scan.source === "manual"
            ? "manual"
            : "scanner";
        void handleScan(event.scan.token, scanSource);
      }
    });

    return () => {
      unsubscribe();
    };
  }, [subscribeScan, handleScan]);

  // Polling scanner freshness/readiness from router
  React.useEffect(() => {
    const checkFreshness = () => {
      try {
        const router = getRouter();
        const sources = router.getRegisteredSources();
        const hid = sources.find((s) => s.id === "hid" || s.id === "scanner") as any;
        if (hid && typeof hid.isFresh === "function") {
          setScannerFresh(hid.isFresh());
        }
        if (hid && typeof hid.isAvailable === "function") {
          void hid.isAvailable().then((avail: boolean) => setScannerReady(avail));
        }
      } catch {
        // Fallback to default ready
      }
    };

    const interval = setInterval(checkFreshness, 3000);
    return () => clearInterval(interval);
  }, [getRouter]);

  const handleReturnLoan = React.useCallback(
    async (loanId: string) => {
      const sid = snapshot.context.sessionId;
      if (!sid) return;

      try {
        const result = await executeReturnLoan({
          sessionId: sid,
          loanId,
        });
        actor.send({ type: "APPLY_SCAN_RESULT", result });
      } catch (err) {
        const problem = await parseProblem(err);
        actor.send({ type: "SET_PROBLEM", problem });
      }
    },
    [actor, snapshot.context.sessionId]
  );

  const handleClose = React.useCallback(async () => {
    const sid = snapshot.context.sessionId;
    if (sid) {
      void executeClose({ sessionId: sid });
    }
    actor.send({ type: "CLOSE" });
  }, [actor, snapshot.context.sessionId]);

  const handleCancel = React.useCallback(async () => {
    const sid = snapshot.context.sessionId;
    if (sid) {
      void executeCancel({ sessionId: sid });
    }
    actor.send({ type: "CANCEL" });
  }, [actor, snapshot.context.sessionId]);

  const cameraSource = React.useMemo(() => {
    try {
      const sources = getRouter().getRegisteredSources();
      return (sources.find((s) => s.id === "camera") as any) ?? null;
    } catch {
      return null;
    }
  }, [getRouter]);

  const hidSource = React.useMemo(() => {
    try {
      const sources = getRouter().getRegisteredSources();
      return (sources.find((s) => s.id === "hid" || s.id === "scanner") as any) ?? null;
    } catch {
      return null;
    }
  }, [getRouter]);

  return {
    state,
    context: snapshot.context,
    kioskName,
    scannerReady,
    scannerFresh,
    isCameraOpen,
    isDiagnosticsOpen,
    cameraSource,
    hidSource,
    setIsCameraOpen,
    setIsDiagnosticsOpen,
    scan: handleScan,
    returnLoan: handleReturnLoan,
    close: handleClose,
    cancel: handleCancel,
  };
}
