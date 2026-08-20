import * as React from "react";
import { useSyncExternalStore } from "react";
import { createActor } from "xstate";
import type { ScanSource, Outcome, SessionMessage } from "@hdms/api-client";
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
import { getKioskConfig, getSessionId, clearSessionId } from "../lib/kiosk-config";
import { parseProblem, type KioskProblem } from "../lib/problem";
import { useScanRouter } from "../lib/scan";
import { playFeedbackSound } from "../lib/audio";
import { getOutcomeFeedback } from "../lib/feedback-config";
import { useConnectivity, onKioskReconnect, resetConnectivityForTesting } from "../lib/connectivity";
import { resetScanSequence } from "../lib/api";

export interface UseKioskSessionOptions {
  actor?: SessionMachineActor;
}

export type OutcomeViewType = "success" | "blocked" | null;

export interface ActiveOutcomeView {
  type: OutcomeViewType;
  outcome?: Outcome | null;
  message?: SessionMessage | null;
  problem?: KioskProblem | null;
}

export function useKioskSession(options?: UseKioskSessionOptions) {
  const config = getKioskConfig();
  const kioskName = config?.kioskName ?? "HDMS Kiosk";
  const kioskId = config?.kioskId ?? "unpaired-kiosk";
  const isMuted = config?.muteEnabled ?? false;

  const { subscribe: subscribeScan, getRouter } = useScanRouter();
  const { isOffline } = useConnectivity();

  const setIsOffline = React.useCallback((offline: boolean) => {
    resetConnectivityForTesting(offline);
  }, []);

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
  const [isAttendantOpen, setIsAttendantOpen] = React.useState(false);
  const [scannerFresh, setScannerFresh] = React.useState(true);
  const [scannerReady, setScannerReady] = React.useState(true);
  const [isOutcomeDismissed, setIsOutcomeDismissed] = React.useState(false);

  // Resync session on reconnect from offline
  React.useEffect(() => {
    return onKioskReconnect(() => {
      const storedSessionId = getSessionId();
      if (storedSessionId) {
        void executeResume({ sessionId: storedSessionId }).then((session) => {
          if (session) {
            actor.send({ type: "RESTORE_SESSION", session });
          } else {
            actor.send({ type: "RESET" });
          }
        });
      }
    });
  }, [actor]);

  const lastOutcome = snapshot.context.lastOutcome;
  const lastProblem = snapshot.context.lastProblem;

  // Reset outcome dismissed flag whenever a new outcome or problem arrives
  const lastOutcomeRef = React.useRef(lastOutcome);
  const lastProblemRef = React.useRef(lastProblem);
  const lastSoundOutcomeRef = React.useRef<string | null>(null);

  React.useEffect(() => {
    if (
      lastOutcome !== lastOutcomeRef.current ||
      lastProblem !== lastProblemRef.current
    ) {
      lastOutcomeRef.current = lastOutcome;
      lastProblemRef.current = lastProblem;
      setIsOutcomeDismissed(false);
    }
  }, [lastOutcome, lastProblem]);

  // Audio feedback triggered from machine transitions, fired once per outcome
  React.useEffect(() => {
    if (lastOutcome) {
      if (lastOutcome.kind === "duplicate") {
        return;
      }
      const outcomeKey = `outcome_${lastOutcome.kind}_${lastOutcome.device?.id ?? ""}_${lastOutcome.dueAt ?? ""}`;
      if (lastSoundOutcomeRef.current !== outcomeKey) {
        lastSoundOutcomeRef.current = outcomeKey;
        const fb = getOutcomeFeedback(lastOutcome.kind);
        playFeedbackSound(fb.soundId, isMuted);
      }
    } else if (lastProblem && lastProblem.kind !== "session-expired") {
      const problemKey = `problem_${lastProblem.kind}_${lastProblem.supportCode ?? ""}`;
      if (lastSoundOutcomeRef.current !== problemKey) {
        lastSoundOutcomeRef.current = problemKey;
        playFeedbackSound("reject", isMuted);
      }
    }
  }, [lastOutcome, lastProblem, isMuted]);

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
        setIsOutcomeDismissed(false);
        actor.send({ type: "APPLY_SCAN_RESULT", result });
      } catch (err: any) {
        if (err?.name === "AbortError" || err?.message?.includes("abort") || err?.message?.includes("Aborted")) {
          return;
        }
        setIsOutcomeDismissed(false);
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
      setIsOutcomeDismissed(false);

      try {
        const result = await executeReturnLoan({
          sessionId: sid,
          loanId,
        });
        actor.send({ type: "APPLY_SCAN_RESULT", result });
      } catch (err: any) {
        if (err?.name === "AbortError" || err?.message?.includes("abort") || err?.message?.includes("Aborted")) {
          return;
        }
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
    setIsOutcomeDismissed(true);
  }, [actor, snapshot.context.sessionId]);

  const handleCancel = React.useCallback(async () => {
    const sid = snapshot.context.sessionId;
    if (sid) {
      void executeCancel({ sessionId: sid });
    }
    actor.send({ type: "CANCEL" });
    setIsOutcomeDismissed(true);
  }, [actor, snapshot.context.sessionId]);

  const dismissOutcome = React.useCallback(() => {
    setIsOutcomeDismissed(true);
    if (snapshot.matches("idle") || !snapshot.context.sessionId) {
      clearSessionId();
      resetScanSequence();
    }
  }, [snapshot, snapshot.context.sessionId]);

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

  const manualSource = React.useMemo(() => {
    try {
      const sources = getRouter().getRegisteredSources();
      return (sources.find((s) => s.id === "manual") as any) ?? null;
    } catch {
      return null;
    }
  }, [getRouter]);

  // Derive transient outcome view
  const outcomeView = React.useMemo<ActiveOutcomeView>(() => {
    if (isOutcomeDismissed) {
      return { type: null };
    }

    const { lastOutcome: currentOutcome, lastMessage, lastProblem: currentProblem } = snapshot.context;

    if (currentOutcome?.kind === "borrowed" || currentOutcome?.kind === "returned") {
      return {
        type: "success",
        outcome: currentOutcome,
        message: lastMessage,
      };
    }

    if (currentOutcome?.kind === "rejected") {
      return {
        type: "blocked",
        outcome: currentOutcome,
        message: lastMessage,
        problem: currentProblem,
      };
    }

    if (currentProblem && currentProblem.kind !== "session-expired") {
      return {
        type: "blocked",
        problem: currentProblem,
        message: lastMessage,
      };
    }

    return { type: null };
  }, [isOutcomeDismissed, snapshot.context]);

  return {
    state,
    context: snapshot.context,
    kioskName,
    scannerReady,
    scannerFresh,
    isCameraOpen,
    isDiagnosticsOpen,
    isAttendantOpen,
    isOffline,
    outcomeView,
    cameraSource,
    hidSource,
    manualSource,
    setIsCameraOpen,
    setIsDiagnosticsOpen,
    setIsAttendantOpen,
    setIsOffline,
    dismissOutcome,
    scan: handleScan,
    returnLoan: handleReturnLoan,
    close: handleClose,
    cancel: handleCancel,
  };
}
