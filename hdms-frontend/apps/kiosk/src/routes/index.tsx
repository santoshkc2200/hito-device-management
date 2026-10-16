import * as React from "react";
import { createRoute } from "@tanstack/react-router";
import { rootRoute } from "./root";
import { useKioskSession } from "@/machine/use-kiosk-session";
import { IdleScreen } from "@/screens/idle-screen";
import { AwaitingUserScreen } from "@/screens/awaiting-user-screen";
import { AwaitingDeviceScreen } from "@/screens/awaiting-device-screen";
import { SessionTransactionList } from "@/components/session-transaction-list";
import { OfflineScreen } from "@/screens/offline-screen";
import { PairingScreen } from "@/screens/pairing-screen";
import { MaintenanceScreen } from "@/screens/maintenance-screen";
import { SessionSummaryScreen } from "@/screens/session-summary-screen";
import { CameraOverlay } from "@/components/camera-overlay";
import { DiagnosticsModal } from "@/components/diagnostics-modal";
import { AttendantModal } from "@/components/attendant-modal";
import { LocaleProvider, DEFAULT_LOCALE } from "@hdms/i18n";
import { useMaintenance } from "@hdms/ui";
import { getKioskConfig, isKioskPaired, subscribeKioskConfig } from "@/lib/kiosk-config";
import { useScreenWakeLock } from "@/lib/wake-lock";
import { useDeferredServiceWorkerUpdate } from "@/lib/sw-update";
import { initReplayEngine } from "@/lib/replay-engine";

export function KioskApp() {
  // Track kiosk pairing state reactively
  const paired = React.useSyncExternalStore(
    subscribeKioskConfig,
    () => isKioskPaired(),
    () => true
  );

  const {
    state,
    context,
    kioskName,
    scannerReady,
    scannerFresh,
    isScanning,
    isCameraOpen,
    isDiagnosticsOpen,
    isAttendantOpen,
    isOffline,
    cameraSource,
    hidSource,
    manualSource,
    setIsCameraOpen,
    setIsDiagnosticsOpen,
    setIsAttendantOpen,
    sessionId,
    applyDueUpdate,
    startScanning,
    scan,
    returnLoan,
    close,
    cancel,
    transactions,
    sessionUser,
    finish,
    isFinished,
    isFinishing,
  } = useKioskSession();

  const underMaintenance = useMaintenance();
  const finishProps = { onFinish: () => void finish(), isFinishing };
  const scannedItems = transactions.length > 0 ? (
    <SessionTransactionList transactions={transactions} sessionId={sessionId} onDueUpdated={applyDueUpdate} isFinishing={isFinishing} />
  ) : null;

  // Re-anchoring the provider on every return to idle is the reset: whatever
  // language the last person selected is discarded with their session.
  const [localeEpoch, setLocaleEpoch] = React.useState(0);
  const previousState = React.useRef(state);
  const previousReview = React.useRef(isFinished);
  React.useEffect(() => {
    if (state === "idle" && !isFinishing && !isFinished && (previousState.current !== "idle" || previousReview.current)) {
      setLocaleEpoch((n) => n + 1);
    }
    previousState.current = state;
    previousReview.current = isFinished;
  }, [state, isFinishing, isFinished]);

  const config = getKioskConfig();
  const defaultLocale = config?.defaultLocale ?? DEFAULT_LOCALE;

  // Screen Wake Lock & deferred SW update during active transactions
  useScreenWakeLock(paired);
  useDeferredServiceWorkerUpdate(state);

  React.useEffect(() => {
    return initReplayEngine();
  }, []);

  const openCameraFallback = React.useCallback(() => {
    void startScanning();
    setIsCameraOpen(true);
  }, [setIsCameraOpen, startScanning]);

  // Kiosk lockdown: suppress context menu
  React.useEffect(() => {
    const handleContextMenu = (e: MouseEvent) => {
      e.preventDefault();
    };
    window.addEventListener("contextmenu", handleContextMenu);
    return () => window.removeEventListener("contextmenu", handleContextMenu);
  }, []);

  // If kiosk is not paired or authorization was revoked (401/403), render PairingScreen
  if (!paired) {
    return (
      <LocaleProvider key={localeEpoch} locale={defaultLocale}>
        <PairingScreen
          initialSupportCode={context.supportCode}
          onPaired={() => {
            // Trigger machine reset or refresh
          }}
        />
      </LocaleProvider>
    );
  }

  return (
    <LocaleProvider key={localeEpoch} locale={defaultLocale}>
      {/* 0. Maintenance outranks everything, offline included */}
      {underMaintenance ? (
        <MaintenanceScreen
          kioskName={kioskName}
          supportCode={context.supportCode}
          onOpenDiagnostics={() => setIsDiagnosticsOpen(true)}
        />
      ) : isFinished ? (
        <SessionSummaryScreen transactions={transactions} user={sessionUser} kioskName={kioskName} onClose={() => void close()} />
      ) : isOffline ? (
        <OfflineScreen
          {...(transactions.length > 0 || context.user ? finishProps : {})}
          kioskName={kioskName}
          supportCode={context.supportCode}
          onToggleCamera={openCameraFallback}
          onOpenDiagnostics={() => setIsDiagnosticsOpen(true)}
          onOpenManualEntry={() => setIsAttendantOpen(true)}
        />
      ) : (
        /* Keep scan results in the active session's list. */
        <>
          {state === "idle" && transactions.length === 0 && (
            <IdleScreen
              kioskName={kioskName}
              supportCode={context.supportCode}
              scannerReady={scannerReady}
              scannerFresh={scannerFresh}
              isScanning={isScanning}
              onStartScanning={() => void startScanning()}
              onStartCamera={openCameraFallback}
              onOpenDiagnostics={() => setIsDiagnosticsOpen(true)}
              onOpenManualEntry={() => setIsAttendantOpen(true)}
            />
          )}

          {!context.user && (state === "awaiting_user" || transactions.length > 0) && (
            <AwaitingUserScreen
              {...finishProps}
              kioskName={kioskName}
              supportCode={context.supportCode}
              scannerReady={scannerReady}
              scannerFresh={scannerFresh}
              pendingDevice={context.pendingDevice}
              expiresAt={context.expiresAt}
              onCancel={cancel}
              onToggleCamera={openCameraFallback}
              onOpenDiagnostics={() => setIsDiagnosticsOpen(true)}
              onOpenManualEntry={() => setIsAttendantOpen(true)}
            >
              {scannedItems}
            </AwaitingUserScreen>
          )}

          {context.user && (
            <AwaitingDeviceScreen
              {...finishProps}
              kioskName={kioskName}
              supportCode={context.supportCode}
              scannerReady={scannerReady}
              scannerFresh={scannerFresh}
              user={context.user}
              openLoans={context.openLoans}
              expiresAt={context.expiresAt}
              onReturnLoan={returnLoan}
              onClose={close}
              onToggleCamera={openCameraFallback}
              onOpenDiagnostics={() => setIsDiagnosticsOpen(true)}
              onOpenManualEntry={() => setIsAttendantOpen(true)}
            >
              {scannedItems}
            </AwaitingDeviceScreen>
          )}
        </>
      )}

      {/* Camera Barcode Viewfinder Modal */}
      <CameraOverlay
        isOpen={isCameraOpen}
        onClose={() => setIsCameraOpen(false)}
        cameraSource={cameraSource}
        onScan={(token) => {
          void scan(token, "camera");
        }}
      />

      {/* Hardware Diagnostics Modal */}
      <DiagnosticsModal
        isOpen={isDiagnosticsOpen}
        onClose={() => setIsDiagnosticsOpen(false)}
        scannerSource={hidSource}
      />

      {/* Attendant Manual Entry PIN Gate & Crockford Keypad Modal */}
      <AttendantModal
        isOpen={isAttendantOpen}
        onClose={() => setIsAttendantOpen(false)}
        manualSource={manualSource}
        onScan={(token) => {
          void scan(token, "manual");
        }}
        onUnpair={() => {
          // Handled via subscribeKioskConfig
        }}
      />
    </LocaleProvider>
  );
}

export const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: KioskApp,
});
