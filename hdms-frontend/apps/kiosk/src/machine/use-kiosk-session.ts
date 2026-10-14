import * as React from "react";
import { useSyncExternalStore } from "react";
import { createActor } from "xstate";
import type { ScanSource, Outcome, SessionMessage, SessionUser, Session } from "@hdms/api-client";
import { CameraSource } from "@hdms/scan";
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
  type SetDueDateResult,
} from "./session-machine";
import { getKioskConfig, getSessionId, clearSessionId } from "../lib/kiosk-config";
import { parseProblem, fromProblemJson, type KioskProblem } from "../lib/problem";
import { useScanRouter } from "../lib/scan";
import { playFeedbackSound } from "../lib/audio";
import { getOutcomeFeedback, outcomeSoundKey } from "../lib/feedback-config";
import { useConnectivity, onKioskReconnect, resetConnectivityForTesting } from "../lib/connectivity";
import { abortActiveSessionRequests, resetScanSequence } from "../lib/api";
import { isUnderMaintenance, subscribeMaintenance } from "@hdms/ui";
import { restoreTransactions, saveTransactions, type SessionTransaction, type PendingScan } from "../lib/session-transactions";

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

  const {
    subscribe: subscribeScan,
    getRouter,
    start: startScanRouter,
    stop: stopScanRouter,
  } = useScanRouter();
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
  const [isScanning, setIsScanning] = React.useState(false);
  const isScanningRef = React.useRef(false);
  const [saved] = React.useState(() => restoreTransactions(getSessionId()));
  const [transactions, setTransactions] = React.useState(saved.transactions);
  const transactionsRef = React.useRef(saved.transactions);
  const pendingRef = React.useRef<PendingScan[]>(saved.pending);
  const nextRowId = React.useRef(Math.max(0, ...saved.transactions.map((row) => row.id)) + 1);
  const queueRef = React.useRef<Promise<void>>(Promise.resolve());
  const generationRef = React.useRef(0);
  const serverSessionIdRef = React.useRef(getSessionId());
  const finishingRef = React.useRef(false);
  const [isFinishing, setIsFinishing] = React.useState(false);
  const [isFinished, setIsFinished] = React.useState(false);
  const [sessionUser, setSessionUser] = React.useState<SessionUser | null>(null);

  const updateTransactions = React.useCallback((update: (rows: SessionTransaction[]) => SessionTransaction[]) => {
    transactionsRef.current = update(transactionsRef.current);
    setTransactions(transactionsRef.current);
    saveTransactions(actor.getSnapshot().context.sessionId, transactionsRef.current, pendingRef.current);
  }, [actor]);

  const clearTransactions = React.useCallback(() => {
    generationRef.current += 1;
    serverSessionIdRef.current = null;
    pendingRef.current = [];
    finishingRef.current = false;
    setIsFinished(false);
    setIsFinishing(false);
    setSessionUser(null);
    updateTransactions(() => []);
    saveTransactions(null, [], []);
  }, [updateTransactions]);

  const markPendingUnconfirmed = React.useCallback(() => {
    // A resumed session may have committed the last card/device request.
    // A fresh scan toggles custody, so never replay an uncertain operation.
    pendingRef.current = [];
    updateTransactions((rows) => rows.map((row) => row.status === "pending" ? { ...row, status: "failed", failureReason: "result-unconfirmed" } : row));
  }, [updateTransactions]);

  const reconcileSession = React.useCallback((session: Session | null) => {
    if (session) {
      serverSessionIdRef.current = session.id;
      setSessionUser(session.user ?? null);
      actor.send({ type: "RESTORE_SESSION", session });
      if (session.user && pendingRef.current.length) markPendingUnconfirmed();
    } else {
      actor.send({ type: "RESET" });
      clearTransactions();
    }
  }, [actor, markPendingUnconfirmed, clearTransactions]);

  const resumeSession = React.useCallback((sessionId: string) => {
    const generation = generationRef.current;
    const task = queueRef.current.then(async () => {
      if (generation !== generationRef.current || finishingRef.current) return;
      const session = await executeResume({ sessionId });
      if (generation === generationRef.current && !finishingRef.current) reconcileSession(session);
    });
    queueRef.current = task.catch(() => {});
    return task;
  }, [reconcileSession]);

  const startScanning = React.useCallback(async () => {
    if (isScanningRef.current || finishingRef.current) return;

    isScanningRef.current = true;
    setIsScanning(true);

    const hasHardwareScanner = getRouter()
      .getRegisteredSources()
      .some((source) => source.id === "scanner" || source.id === "hid");
    setScannerReady(hasHardwareScanner);
    await startScanRouter();
  }, [getRouter, startScanRouter]);

  const stopScanning = React.useCallback(async () => {
    isScanningRef.current = false;
    setIsScanning(false);
    await stopScanRouter();
  }, [stopScanRouter]);

  const previousStateRef = React.useRef(state);
  React.useEffect(() => {
    if (state !== "idle" && !isScanningRef.current) {
      void startScanning();
    } else if (previousStateRef.current !== "idle" && state === "idle") {
      void stopScanning();
    }
    previousStateRef.current = state;
  }, [state, startScanning, stopScanning]);

  const ledgerStateRef = React.useRef(state);
  React.useEffect(() => {
    if (ledgerStateRef.current !== "idle" && state === "idle" && !snapshot.context.lastOutcome && !finishingRef.current) {
      clearTransactions();
    }
    ledgerStateRef.current = state;
  }, [state, snapshot.context.lastOutcome, clearTransactions]);

  // Resync session on reconnect from offline
  React.useEffect(() => {
    return onKioskReconnect(() => {
      const storedSessionId = getSessionId();
      if (storedSessionId) {
        void resumeSession(storedSessionId);
      }
    });
  }, [resumeSession]);

  // A restore is about to rewind the data: drop whatever session is open.
  // The server-side session expires on its own, and nothing is queued.
  React.useEffect(() => {
    return subscribeMaintenance(() => {
      if (!isUnderMaintenance()) return;
      abortActiveSessionRequests();
      clearSessionId();
      actor.send({ type: "RESET" });
      clearTransactions();
    });
  }, [actor, clearTransactions]);

  const lastOutcome = snapshot.context.lastOutcome;
  const lastProblem = snapshot.context.lastProblem;

  // Reset outcome dismissed flag whenever a new outcome or problem arrives
  const lastOutcomeRef = React.useRef(lastOutcome);
  const lastProblemRef = React.useRef(lastProblem);
  const lastSoundOutcomeRef = React.useRef<string | null>(null);

  React.useEffect(() => {
    const isNewOutcome =
      lastOutcome !== lastOutcomeRef.current &&
      !(
        lastOutcome &&
        lastOutcomeRef.current &&
        lastOutcome.loanId === lastOutcomeRef.current.loanId &&
        lastOutcome.kind === lastOutcomeRef.current.kind
      );
    if (isNewOutcome || lastProblem !== lastProblemRef.current) {
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
      const outcomeKey = outcomeSoundKey(lastOutcome);
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
      void resumeSession(storedSessionId);
    }
  }, [resumeSession, state]);

  // Foreground visibility reconciliation
  React.useEffect(() => {
    const generation = generationRef.current;
    const sessionId = snapshot.context.sessionId;
    return setupVisibilityReconciliation({ send: (event) => {
      if (generation !== generationRef.current || finishingRef.current || actor.getSnapshot().context.sessionId !== sessionId) return;
      if (event.type === "RESTORE_SESSION") reconcileSession(event.session as Session);
      else if (event.type === "RESET") reconcileSession(null);
      else actor.send(event);
    } }, resumeSession);
  }, [actor, reconcileSession, resumeSession, snapshot.context.sessionId]);

  React.useEffect(() => () => {
    // Responses belonging to an unmounted kiosk must not update its ledger.
    generationRef.current += 1;
    isScanningRef.current = false;
  }, []);

  // Both scan sources share a queue. Read the actor at execution time so a
  // fast second scan uses the session created by the first one.
  const processScan = React.useCallback(
    async function processQueuedScan(token: string, source: ScanSource, generation: number, pendingRowId?: number): Promise<void> {
      if (generation !== generationRef.current || isUnderMaintenance()) return;
      const before = actor.getSnapshot().context;
      const latestPending = pendingRowId === undefined && !before.user ? pendingRef.current.at(-1) : undefined;
      try {
        if (!before.sessionId && serverSessionIdRef.current && !before.lastOutcome && finishingRef.current) {
          throw fromProblemJson({ type: "https://hdms/problems/session-expired", title: "session-expired", status: 410 });
        }
        const result = await executeScan({
          sessionId: before.sessionId ?? serverSessionIdRef.current,
          kioskId,
          token,
          source,
          preferredDueAt: before.preferredDueAt,
        });
        if (generation !== generationRef.current || isUnderMaintenance()) return;
        serverSessionIdRef.current = result.session.id;
        setIsOutcomeDismissed(false);
        actor.send({ type: "APPLY_SCAN_RESULT", result });
        if (result.session.user) setSessionUser(result.session.user);
        const resolvedPending = latestPending && result.outcome.device?.id === latestPending.device.id;
        if (result.session.user && !before.user && latestPending && !resolvedPending) markPendingUnconfirmed();

        if (result.outcome.kind === "device_pending" && result.outcome.device) {
          // Repeated scans of a pending device are one attempt, even when
          // scanned through a different barcode/QR credential.
          const existing = pendingRef.current.find((pending) => pending.device.id === result.outcome.device!.id);
          const pending = { rowId: existing?.rowId ?? nextRowId.current++, token, source, device: result.outcome.device };
          pendingRef.current = [...pendingRef.current.filter((item) => item.rowId !== pending.rowId), pending];
          updateTransactions((rows) => existing ? rows : [...rows, { id: pending.rowId, device: pending.device, status: "pending" }]);
        } else if (["borrowed", "returned", "reservation_collected", "rejected"].includes(result.outcome.kind)) {
          const rowId = pendingRowId ?? (result.session.user && !before.user && resolvedPending ? latestPending?.rowId : undefined);
          const device = result.outcome.device ?? (rowId === undefined ? null : transactionsRef.current.find((row) => row.id === rowId)?.device);
          const row: SessionTransaction = {
            id: rowId ?? nextRowId.current++, device,
            status: result.outcome.kind === "rejected" ? "failed" : result.outcome.kind === "returned" ? "returned" : "borrowed",
            message: result.message, dueAt: result.outcome.dueAt, loanId: result.outcome.loanId,
          };
          if (rowId !== undefined) pendingRef.current = pendingRef.current.filter((pending) => pending.rowId !== rowId);
          updateTransactions((rows) => rowId === undefined ? [...rows, row] : rows.map((old) => old.id === rowId ? row : old));
        }

        // The server resolves the latest pending device on the card scan.
        // Resolve the rest through the same authoritative API, one by one.
        if (result.session.user && pendingRef.current.length > 0 && pendingRowId === undefined) {
          for (const pending of [...pendingRef.current]) {
            await processQueuedScan(pending.token, pending.source, generation, pending.rowId);
          }
        }
      } catch (err: any) {
        if (err?.name === "AbortError" || err?.message?.includes("abort") || err?.message?.includes("Aborted") || typeof err === "string" && err.includes("Session terminated")) {
          if (finishingRef.current && generation === generationRef.current && !isUnderMaintenance()) {
            const rowId = pendingRowId ?? nextRowId.current++;
            const row: SessionTransaction = { id: rowId, status: "failed", failureReason: "result-unconfirmed", device: transactionsRef.current.find((old) => old.id === rowId)?.device };
            pendingRef.current = pendingRef.current.filter((pending) => pending.rowId !== rowId);
            updateTransactions((rows) => pendingRowId === undefined ? [...rows, row] : rows.map((old) => old.id === rowId ? row : old));
          }
          return;
        }
        // The maintenance screen is already up; this is not a refusal.
        if (isUnderMaintenance()) return;
        if (generation !== generationRef.current) return;
        setIsOutcomeDismissed(false);
        const problem = await parseProblem(err);
        actor.send({ type: "SET_PROBLEM", problem });
        const rowId = pendingRowId ?? nextRowId.current++;
        const row: SessionTransaction = { id: rowId, status: "failed", problem, ...(problem.kind === "session-expired" ? { failureReason: "session-expired" } : {}), device: transactionsRef.current.find((old) => old.id === rowId)?.device };
        pendingRef.current = pendingRef.current.filter((pending) => pending.rowId !== rowId);
        updateTransactions((rows) => pendingRowId === undefined ? [...rows, row] : rows.map((old) => old.id === rowId ? row : old));
      }
    },
    [actor, kioskId, updateTransactions, markPendingUnconfirmed]
  );

  const enqueue = React.useCallback((operation: (generation: number) => Promise<void>) => {
    const generation = generationRef.current;
    const task = queueRef.current.then(() => operation(generation));
    queueRef.current = task.catch(() => {});
    return task;
  }, []);

  const handleScan = React.useCallback((token: string, source: ScanSource = "scanner") => {
    if (!isScanningRef.current || finishingRef.current || isUnderMaintenance()) return Promise.resolve();
    return enqueue((generation) => processScan(token, source, generation));
  }, [enqueue, processScan]);

  const handleFinish = React.useCallback(async () => {
    if (finishingRef.current) return;
    finishingRef.current = true;
    setIsFinishing(true);
    setIsCameraOpen(false);
    const generation = generationRef.current;
    await stopScanning();
    let queue: Promise<void>;
    do {
      queue = queueRef.current;
      await queue;
    } while (queue !== queueRef.current);
    if (generation !== generationRef.current) return;
    pendingRef.current = [];
    updateTransactions((rows) => rows.map((row) => row.status === "pending" ? { ...row, status: "failed", failureReason: "user-required" } : row));
    setIsOutcomeDismissed(true);
    setIsFinishing(false);
    setIsFinished(true);
  }, [stopScanning, updateTransactions, setIsCameraOpen]);

  // Subscribe to ScanRouter events
  React.useEffect(() => {
    const unsubscribe = subscribeScan((event) => {
      if (event.kind === "source-state" && (event.id === "scanner" || event.id === "hid")) {
        setScannerReady(event.available);
        if (!event.available) setScannerFresh(false);
      } else if (event.kind === "scan") {
        const scanSource: ScanSource =
          event.scan.source === "camera"
            ? "camera"
            : event.scan.source === "manual"
            ? "manual"
            : "scanner";
        setScannerReady(true);
        setScannerFresh(true);
        void handleScan(event.scan.token, scanSource);
      } else if (event.kind === "invalid" && isScanningRef.current && !finishingRef.current && !isUnderMaintenance()) {
        void enqueue(async (generation) => {
          if (generation !== generationRef.current) return;
          const problem = fromProblemJson({ type: "https://hdms/problems/invalid-token-format", title: "invalid-token-format", status: 400 });
          actor.send({ type: "SET_PROBLEM", problem });
          setIsOutcomeDismissed(false);
          updateTransactions((rows) => [...rows, { id: nextRowId.current++, status: "failed", problem }]);
        });
      }
    });

    return () => {
      unsubscribe();
    };
  }, [subscribeScan, handleScan, actor, enqueue, updateTransactions]);

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
    (loanId: string) => {
      if (finishingRef.current || isUnderMaintenance()) return Promise.resolve();
      const acceptedLoan = actor.getSnapshot().context.openLoans.find((item) => item.id === loanId);
      return enqueue(async (generation) => {
        const context = actor.getSnapshot().context;
        const sid = context.sessionId ?? serverSessionIdRef.current;
        if (!sid || generation !== generationRef.current) return;
        const loan = context.openLoans.find((item) => item.id === loanId) ?? acceptedLoan;
        const device = loan ? { id: loan.deviceId, name: loan.deviceName, assetTag: loan.assetTag } : null;
        setIsOutcomeDismissed(false);

        try {
          if (!context.sessionId && !context.lastOutcome && finishingRef.current) {
            throw fromProblemJson({ type: "https://hdms/problems/session-expired", title: "session-expired", status: 410 });
          }
          const result = await executeReturnLoan({ sessionId: sid, loanId });
          if (generation !== generationRef.current || isUnderMaintenance()) return;
          actor.send({ type: "APPLY_SCAN_RESULT", result });
          if (result.outcome.kind !== "duplicate") {
            updateTransactions((rows) => [...rows, {
              id: nextRowId.current++, device: result.outcome.device ?? device,
              status: result.outcome.kind === "returned" ? "returned" : "failed",
              message: result.message, loanId,
            }]);
          }
        } catch (err: any) {
          if (isUnderMaintenance() || generation !== generationRef.current) return;
          if (err?.name === "AbortError" || err?.message?.includes("abort") || err?.message?.includes("Aborted") || typeof err === "string" && err.includes("Session terminated")) {
            if (finishingRef.current) updateTransactions((rows) => [...rows, { id: nextRowId.current++, status: "failed", device, failureReason: "result-unconfirmed" }]);
            return;
          }
          const problem = await parseProblem(err);
          actor.send({ type: "SET_PROBLEM", problem });
          updateTransactions((rows) => [...rows, { id: nextRowId.current++, status: "failed", device, problem, ...(problem.kind === "session-expired" ? { failureReason: "session-expired" } : {}) }]);
        }
      });
    },
    [actor, enqueue, updateTransactions]
  );

  const handleClose = React.useCallback(async () => {
    const sid = actor.getSnapshot().context.sessionId ?? serverSessionIdRef.current;
    clearTransactions();
    await stopScanning();
    if (sid) {
      void executeClose({ sessionId: sid }).catch(() => {});
    }
    actor.send({ type: "CLOSE" });
    setIsOutcomeDismissed(true);
  }, [actor, clearTransactions, stopScanning]);

  const handleCancel = React.useCallback(async () => {
    const sid = actor.getSnapshot().context.sessionId ?? serverSessionIdRef.current;
    clearTransactions();
    await stopScanning();
    if (sid) {
      void executeCancel({ sessionId: sid }).catch(() => {});
    }
    actor.send({ type: "CANCEL" });
    setIsOutcomeDismissed(true);
  }, [actor, clearTransactions, stopScanning]);

  const dismissOutcome = React.useCallback(() => {
    setIsOutcomeDismissed(true);
    if (snapshot.matches("idle") || !snapshot.context.sessionId) {
      clearSessionId();
      resetScanSequence();
    }
  }, [snapshot]);

  const applyDueUpdate = React.useCallback(
    (update: Extract<SetDueDateResult, { ok: true }>) => {
      actor.send({ type: "LOAN_DUE_UPDATED", ...update });
      updateTransactions((rows) => rows.map((row) => row.loanId === update.loanId ? { ...row, dueAt: update.dueAt } : row));
    },
    [actor, updateTransactions]
  );

  const cameraEnabled = config?.enabledSources?.includes("camera") ?? true;
  const cameraSource = React.useMemo(
    () => (cameraEnabled ? new CameraSource() : null),
    [cameraEnabled]
  );

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

    if (currentOutcome?.kind === "borrowed" || currentOutcome?.kind === "returned" || currentOutcome?.kind === "reservation_collected") {
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
    transactions,
    sessionUser,
    isFinishing,
    isFinished,
    finish: handleFinish,
    context: snapshot.context,
    sessionId: snapshot.context.sessionId,
    applyDueUpdate,
    kioskName,
    scannerReady,
    scannerFresh,
    isScanning,
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
    startScanning,
    scan: handleScan,
    returnLoan: handleReturnLoan,
    close: handleClose,
    cancel: handleCancel,
  };
}
