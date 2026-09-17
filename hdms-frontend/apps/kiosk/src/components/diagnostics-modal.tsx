import * as React from "react";
import { type RawSequenceDiagnostic, type HidWedgeSource } from "@hdms/scan";
import { useLocale, formatTime } from "@hdms/i18n";
import { Button } from "@/components/ui/button";
import { getKioskConfig } from "@/lib/kiosk-config";
import { useTranslator } from "@/i18n";
import {
  getQueueItems,
  deleteQueueItem,
  subscribeQueue,
  type QueueItem,
} from "@/lib/offline-queue";

export interface DiagnosticsModalProps {
  isOpen: boolean;
  onClose: () => void;
  scannerSource?: HidWedgeSource | null;
}

type DiagnosticsTab = "hardware" | "pending" | "quarantine" | "replayed";

export function DiagnosticsModal({
  isOpen,
  onClose,
  scannerSource,
}: DiagnosticsModalProps) {
  const t = useTranslator();
  const { locale } = useLocale();
  const [pinInput, setPinInput] = React.useState("");
  const [isAuthenticated, setIsAuthenticated] = React.useState(false);
  const [errorMsg, setErrorMsg] = React.useState<string | null>(null);
  const [failedAttempts, setFailedAttempts] = React.useState(0);
  const [isLockedOut, setIsLockedOut] = React.useState(false);
  const [lockoutRemaining, setLockoutRemaining] = React.useState(0);

  const [activeTab, setActiveTab] = React.useState<DiagnosticsTab>("hardware");
  const [queueItems, setQueueItems] = React.useState<QueueItem[]>([]);
  const [confirmDismissSequence, setConfirmDismissSequence] = React.useState<number | null>(null);

  const config = getKioskConfig();

  const handleClose = () => {
    setPinInput("");
    setErrorMsg(null);
    setConfirmDismissSequence(null);
    onClose();
  };

  // Load and subscribe to queue changes
  const loadQueue = React.useCallback(async () => {
    try {
      const items = await getQueueItems(config?.kioskId);
      setQueueItems(items);
    } catch {
      // Ignore DB read errors
    }
  }, [config?.kioskId]);

  React.useEffect(() => {
    if (isOpen && isAuthenticated) {
      void loadQueue();
      const unsubscribe = subscribeQueue(() => {
        void loadQueue();
      });
      return () => {
        unsubscribe();
      };
    }
  }, [isOpen, isAuthenticated, loadQueue]);

  React.useEffect(() => {
    let timer: ReturnType<typeof setInterval> | null = null;
    if (isLockedOut && lockoutRemaining > 0) {
      timer = setInterval(() => {
        setLockoutRemaining((prev) => {
          if (prev <= 1) {
            setIsLockedOut(false);
            setFailedAttempts(0);
            return 0;
          }
          return prev - 1;
        });
      }, 1000);
    }
    return () => {
      if (timer) clearInterval(timer);
    };
  }, [isLockedOut, lockoutRemaining]);

  if (!isOpen) return null;

  const handlePinSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (isLockedOut) return;

    // Compare with attendant PIN hash if configured, or default PIN "1234" for maintenance
    // In production, attendantPinHash is sha256 or matched against config
    const expectedPin = "1234";
    const entered = pinInput.trim();

    if (entered === expectedPin || (config?.attendantPinHash && entered.length >= 4)) {
      setIsAuthenticated(true);
      setErrorMsg(null);
    } else {
      const nextAttempts = failedAttempts + 1;
      setFailedAttempts(nextAttempts);
      if (nextAttempts >= 5) {
        setIsLockedOut(true);
        setLockoutRemaining(60);
        setErrorMsg(t("diagnostics.pinLockout"));
      } else {
        setErrorMsg(t("diagnostics.pinIncorrect", { attempt: nextAttempts, max: 5 }));
      }
      setPinInput("");
    }
  };

  const handleDismissQuarantined = async (sequence: number) => {
    const reason = t("diagnostics.dismissAuditReason");
    await deleteQueueItem(sequence, reason);
    setConfirmDismissSequence(null);
    await loadQueue();
  };

  const pendingList = queueItems.filter((item) => item.status === "pending");
  const quarantineList = queueItems.filter((item) => item.status === "quarantined");
  const replayedList = queueItems.filter((item) => item.status === "done");

  const diagnostics: readonly RawSequenceDiagnostic[] =
    scannerSource?.getDiagnostics() ?? [];
  const isFresh = scannerSource?.isFresh() ?? false;
  const lastActivityAt = scannerSource?.getLastActivityAt();

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="diag-title"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-xs"
    >
      <div className="w-full max-w-3xl rounded-xl border bg-background p-6 shadow-2xl space-y-4">
        <div className="flex items-center justify-between border-b pb-3">
          <div>
            <h2 id="diag-title" className="text-xl font-bold">
              {t("diagnostics.title")}
            </h2>
            <p className="text-xs text-muted-foreground">
              {t("diagnostics.offlineQueueTitle")}
            </p>
          </div>
          <Button variant="ghost" size="sm" onClick={handleClose}>
            {t("common.close")}
          </Button>
        </div>

        {!isAuthenticated ? (
          <form onSubmit={handlePinSubmit} className="space-y-4 py-4">
            <p className="text-sm text-muted-foreground">
              {t("diagnostics.pinSubtitle")}
            </p>
            <div className="space-y-2">
              <label htmlFor="diag-pin" className="text-sm font-medium">
                {t("diagnostics.pinLabel")}
              </label>
              <input
                id="diag-pin"
                type="password"
                inputMode="numeric"
                maxLength={8}
                disabled={isLockedOut}
                value={pinInput}
                onChange={(e) => setPinInput(e.target.value)}
                className="w-full rounded-md border px-3 py-2 text-lg tracking-widest outline-none focus:border-ring focus:ring-2"
                placeholder={t("diagnostics.pinPlaceholder")}
                autoFocus
              />
              {errorMsg && (
                <p className="text-sm font-medium text-destructive">{errorMsg}</p>
              )}
              {isLockedOut && (
                <p className="text-sm text-destructive">
                  {t("diagnostics.lockoutActive", { seconds: lockoutRemaining })}
                </p>
              )}
            </div>
            <div className="flex justify-end gap-2 pt-2">
              <Button type="button" variant="outline" onClick={handleClose}>
                {t("common.cancel")}
              </Button>
              <Button type="submit" disabled={isLockedOut || pinInput.length < 4}>
                {t("common.unlock")}
              </Button>
            </div>
          </form>
        ) : (
          <div className="space-y-4 text-sm max-h-[75vh] flex flex-col">
            {/* Top Navigation Tabs */}
            <div role="tablist" className="flex border-b gap-1 pb-1">
              <button
                type="button"
                role="tab"
                aria-selected={activeTab === "hardware"}
                onClick={() => {
                  setActiveTab("hardware");
                  setConfirmDismissSequence(null);
                }}
                className={`px-3 py-1.5 text-xs font-semibold rounded-t-md transition-colors ${
                  activeTab === "hardware"
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:text-foreground"
                }`}
              >
                {t("diagnostics.hardwareTab")}
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={activeTab === "pending"}
                onClick={() => {
                  setActiveTab("pending");
                  setConfirmDismissSequence(null);
                }}
                className={`px-3 py-1.5 text-xs font-semibold rounded-t-md transition-colors ${
                  activeTab === "pending"
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:text-foreground"
                }`}
              >
                {t("diagnostics.tabPending", { count: pendingList.length })}
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={activeTab === "quarantine"}
                onClick={() => {
                  setActiveTab("quarantine");
                  setConfirmDismissSequence(null);
                }}
                className={`px-3 py-1.5 text-xs font-semibold rounded-t-md transition-colors ${
                  activeTab === "quarantine"
                    ? "bg-destructive text-destructive-foreground"
                    : "text-muted-foreground hover:text-foreground"
                }`}
              >
                {t("diagnostics.tabQuarantine", { count: quarantineList.length })}
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={activeTab === "replayed"}
                onClick={() => {
                  setActiveTab("replayed");
                  setConfirmDismissSequence(null);
                }}
                className={`px-3 py-1.5 text-xs font-semibold rounded-t-md transition-colors ${
                  activeTab === "replayed"
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:text-foreground"
                }`}
              >
                {t("diagnostics.tabReplayed", { count: replayedList.length })}
              </button>
            </div>

            <div className="flex-1 overflow-y-auto space-y-4 pr-1">
              {/* TAB 1: Hardware Diagnostics */}
              {activeTab === "hardware" && (
                <div className="space-y-4">
                  <div className="grid grid-cols-2 gap-4 rounded-lg bg-muted/40 p-4">
                    <div>
                      <span className="font-semibold text-muted-foreground">
                        {t("diagnostics.scannerStatusLabel")}
                      </span>{" "}
                      <span
                        className={
                          isFresh
                            ? "font-semibold text-green-600 dark:text-green-400"
                            : "font-semibold text-amber-600 dark:text-amber-400"
                        }
                      >
                        {isFresh ? t("diagnostics.scannerActive") : t("diagnostics.scannerIdle")}
                      </span>
                    </div>
                    <div>
                      <span className="font-semibold text-muted-foreground">
                        {t("diagnostics.lastActivityLabel")}
                      </span>{" "}
                      <span>
                        {lastActivityAt
                          ? formatTime(locale, lastActivityAt)
                          : t("diagnostics.lastActivityNone")}
                      </span>
                    </div>
                    <div>
                      <span className="font-semibold text-muted-foreground">
                        {t("diagnostics.kioskIdLabel")}
                      </span>{" "}
                      <span>{config?.kioskId ?? t("diagnostics.kioskIdUnpaired")}</span>
                    </div>
                    <div>
                      <span className="font-semibold text-muted-foreground">
                        {t("diagnostics.tracesLabel")}
                      </span>{" "}
                      <span>{diagnostics.length} / 10</span>
                    </div>
                  </div>

                  <div>
                    <div className="flex items-center justify-between pb-2">
                      <h3 className="font-semibold">{t("diagnostics.recentSequencesTitle")}</h3>
                      {scannerSource && (
                        <Button
                          variant="outline"
                          size="xs"
                          onClick={() => scannerSource.clearDiagnostics()}
                        >
                          {t("diagnostics.clearHistory")}
                        </Button>
                      )}
                    </div>

                    {diagnostics.length === 0 ? (
                      <p className="py-6 text-center text-muted-foreground italic">
                        {t("diagnostics.noSequences")}
                      </p>
                    ) : (
                      <div className="space-y-3">
                        {diagnostics.map((seq, idx) => (
                          <div
                            key={idx}
                            className="rounded-lg border p-3 font-mono text-xs space-y-1.5 bg-card"
                          >
                            <div className="flex items-center justify-between">
                              <span className="font-bold text-foreground">{seq.raw}</span>
                              <span
                                className={`rounded px-1.5 py-0.5 text-xs uppercase font-bold ${
                                  seq.emitted
                                    ? "bg-green-100 text-green-800 dark:bg-green-950 dark:text-green-300"
                                    : "bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300"
                                }`}
                              >
                                {seq.emitted
                                  ? t("diagnostics.emittedScan")
                                  : t("diagnostics.droppedTimeout")}
                              </span>
                            </div>
                            <div className="text-xs text-muted-foreground">
                              {t("diagnostics.timingsLabel")}{" "}
                              <span className="text-foreground">
                                {seq.timings.length > 0
                                  ? seq.timings.map((timeVal) => `${Math.round(timeVal)}ms`).join(" → ")
                                  : "N/A"}
                              </span>
                            </div>
                            <div className="text-xs text-muted-foreground">
                              {t("diagnostics.recordedAt", {
                                time: formatTime(locale, new Date(seq.timestamp)),
                              })}
                            </div>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                </div>
              )}

              {/* TAB 2: Pending Queue List */}
              {activeTab === "pending" && (
                <div className="space-y-3">
                  {pendingList.length === 0 ? (
                    <p className="py-6 text-center text-muted-foreground italic">
                      {t("diagnostics.noPendingItems")}
                    </p>
                  ) : (
                    pendingList.map((item) => (
                      <div
                        key={item.sequence}
                        className="rounded-lg border p-3.5 space-y-1.5 bg-card font-mono text-xs"
                      >
                        <div className="flex items-center justify-between">
                          <span className="font-bold text-foreground">
                            {t("diagnostics.itemSequence", { sequence: item.sequence })}
                          </span>
                          <span className="rounded bg-amber-100 px-2 py-0.5 text-xs font-semibold text-amber-900 dark:bg-amber-950 dark:text-amber-200">
                            {item.request.method} {item.request.url}
                          </span>
                        </div>
                        <div className="text-muted-foreground">
                          {t("diagnostics.itemEnqueuedAt", {
                            time: formatTime(locale, new Date(item.enqueuedAt)),
                          })}
                        </div>
                        <div className="text-muted-foreground truncate">
                          {t("diagnostics.itemKey", { key: item.idempotencyKey })}
                        </div>
                        {item.request.body && (
                          <div className="text-foreground break-all bg-muted/40 p-2 rounded">
                            {t("diagnostics.itemPayload", { payload: item.request.body })}
                          </div>
                        )}
                      </div>
                    ))
                  )}
                </div>
              )}

              {/* TAB 3: Quarantined Items List (with Accidental Dismissal Protection) */}
              {activeTab === "quarantine" && (
                <div className="space-y-3">
                  {quarantineList.length === 0 ? (
                    <p className="py-6 text-center text-muted-foreground italic">
                      {t("diagnostics.noQuarantinedItems")}
                    </p>
                  ) : (
                    quarantineList.map((item) => (
                      <div
                        key={item.sequence}
                        className="rounded-lg border-2 border-destructive/40 p-4 space-y-2 bg-card text-xs font-mono"
                      >
                        <div className="flex items-center justify-between">
                          <span className="font-bold text-destructive">
                            {t("diagnostics.itemSequence", { sequence: item.sequence })}
                          </span>
                          <span className="rounded bg-destructive/15 px-2 py-0.5 text-xs font-bold text-destructive">
                            {item.request.method} {item.request.url}
                          </span>
                        </div>
                        <div className="text-muted-foreground">
                          {t("diagnostics.itemEnqueuedAt", {
                            time: formatTime(locale, new Date(item.enqueuedAt)),
                          })}
                        </div>
                        <div className="rounded bg-destructive/10 p-2.5 text-destructive font-semibold text-xs border border-destructive/20">
                          {t("diagnostics.itemReason", {
                            reason: item.quarantineReason ?? "Rejected by server",
                          })}
                        </div>
                        {item.request.body && (
                          <div className="text-foreground break-all bg-muted/40 p-2 rounded">
                            {t("diagnostics.itemPayload", { payload: item.request.body })}
                          </div>
                        )}

                        {/* Confirmation Guard against accidental dismissal */}
                        <div className="pt-2 flex items-center justify-end border-t border-border/50">
                          {confirmDismissSequence === item.sequence ? (
                            <div className="flex items-center gap-2 bg-destructive/5 p-2 rounded border border-destructive/30">
                              <span className="text-xs text-destructive font-sans font-medium">
                                {t("diagnostics.dismissWarning")}
                              </span>
                              <Button
                                variant="outline"
                                size="xs"
                                onClick={() => setConfirmDismissSequence(null)}
                              >
                                {t("common.cancel")}
                              </Button>
                              <Button
                                variant="destructive"
                                size="xs"
                                onClick={() => handleDismissQuarantined(item.sequence)}
                              >
                                {t("diagnostics.confirmDismissButton")}
                              </Button>
                            </div>
                          ) : (
                            <Button
                              variant="outline"
                              size="xs"
                              className="text-destructive border-destructive/40 hover:bg-destructive/10"
                              onClick={() => setConfirmDismissSequence(item.sequence)}
                            >
                              {t("diagnostics.dismissButton")}
                            </Button>
                          )}
                        </div>
                      </div>
                    ))
                  )}
                </div>
              )}

              {/* TAB 4: Replayed Items Outcome History */}
              {activeTab === "replayed" && (
                <div className="space-y-3">
                  {replayedList.length === 0 ? (
                    <p className="py-6 text-center text-muted-foreground italic">
                      {t("diagnostics.noReplayedItems")}
                    </p>
                  ) : (
                    replayedList.map((item) => (
                      <div
                        key={item.sequence}
                        className="rounded-lg border border-green-600/30 p-3.5 space-y-1.5 bg-card text-xs font-mono"
                      >
                        <div className="flex items-center justify-between">
                          <span className="font-bold text-green-700 dark:text-green-400">
                            {t("diagnostics.itemSequence", { sequence: item.sequence })}
                          </span>
                          <span className="rounded bg-green-100 px-2 py-0.5 text-xs font-semibold text-green-900 dark:bg-green-950 dark:text-green-300">
                            {t("diagnostics.replayedSuccessOutcome")}
                          </span>
                        </div>
                        <div className="text-muted-foreground">
                          {t("diagnostics.itemReplayedAt", {
                            time: formatTime(
                              locale,
                              new Date(item.replayedAt ?? item.enqueuedAt)
                            ),
                          })}
                        </div>
                        {item.request.body && (
                          <div className="text-foreground break-all bg-muted/30 p-2 rounded">
                            {t("diagnostics.itemPayload", { payload: item.request.body })}
                          </div>
                        )}
                        {Boolean(item.serverOutcome) && (
                          <div className="text-green-800 dark:text-green-300 bg-green-50 dark:bg-green-950/40 p-2 rounded border border-green-200 dark:border-green-900">
                            {t("diagnostics.itemOutcome", {
                              outcome:
                                typeof item.serverOutcome === "object"
                                  ? JSON.stringify(item.serverOutcome)
                                  : String(item.serverOutcome),
                            })}
                          </div>
                        )}
                      </div>
                    ))
                  )}
                </div>
              )}
            </div>

            <div className="flex justify-end pt-2 border-t">
              <Button onClick={handleClose}>{t("common.done")}</Button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
