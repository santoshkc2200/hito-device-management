import { createRoute } from "@tanstack/react-router";
import { rootRoute } from "./root";
import { useKioskSession } from "@/machine/use-kiosk-session";
import { IdleScreen } from "@/screens/idle-screen";
import { AwaitingUserScreen } from "@/screens/awaiting-user-screen";
import { AwaitingDeviceScreen } from "@/screens/awaiting-device-screen";
import { SuccessScreen } from "@/screens/success-screen";
import { BlockedScreen } from "@/screens/blocked-screen";
import { OfflineScreen } from "@/screens/offline-screen";
import { CameraOverlay } from "@/components/camera-overlay";
import { DiagnosticsModal } from "@/components/diagnostics-modal";
import { AttendantModal } from "@/components/attendant-modal";

export function KioskApp() {
  const {
    state,
    context,
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
    dismissOutcome,
    scan,
    returnLoan,
    close,
    cancel,
  } = useKioskSession();

  return (
    <>
      {/* 1. Offline Mode Display */}
      {isOffline ? (
        <OfflineScreen
          kioskName={kioskName}
          supportCode={context.supportCode}
          onToggleCamera={() => setIsCameraOpen(true)}
          onOpenDiagnostics={() => setIsDiagnosticsOpen(true)}
          onOpenManualEntry={() => setIsAttendantOpen(true)}
        />
      ) : outcomeView.type === "success" && outcomeView.outcome ? (
        /* 2. Success Outcome Screen */
        <SuccessScreen
          kind={outcomeView.outcome.kind}
          device={outcomeView.outcome.device ?? context.pendingDevice}
          message={outcomeView.message}
          dueAt={outcomeView.outcome.dueAt}
          itemCount={context.openLoans.length || 1}
          onDone={dismissOutcome}
          onScanAnother={dismissOutcome}
          kioskName={kioskName}
          supportCode={context.supportCode}
          scannerReady={scannerReady}
          scannerFresh={scannerFresh}
          onToggleCamera={() => setIsCameraOpen(true)}
          onOpenDiagnostics={() => setIsDiagnosticsOpen(true)}
          onOpenManualEntry={() => setIsAttendantOpen(true)}
        />
      ) : outcomeView.type === "blocked" ? (
        /* 3. Blocked / Refusal Outcome Screen */
        <BlockedScreen
          message={outcomeView.message}
          problem={outcomeView.problem}
          onDismiss={dismissOutcome}
          kioskName={kioskName}
          supportCode={context.supportCode}
          scannerReady={scannerReady}
          scannerFresh={scannerFresh}
          onToggleCamera={() => setIsCameraOpen(true)}
          onOpenDiagnostics={() => setIsDiagnosticsOpen(true)}
          onOpenManualEntry={() => setIsAttendantOpen(true)}
        />
      ) : (
        /* 4. Primary Interactive Screens */
        <>
          {state === "idle" && (
            <IdleScreen
              kioskName={kioskName}
              supportCode={context.supportCode}
              scannerReady={scannerReady}
              scannerFresh={scannerFresh}
              onToggleCamera={() => setIsCameraOpen(true)}
              onOpenDiagnostics={() => setIsDiagnosticsOpen(true)}
              onOpenManualEntry={() => setIsAttendantOpen(true)}
            />
          )}

          {state === "awaiting_user" && (
            <AwaitingUserScreen
              kioskName={kioskName}
              supportCode={context.supportCode}
              scannerReady={scannerReady}
              scannerFresh={scannerFresh}
              pendingDevice={context.pendingDevice}
              expiresAt={context.expiresAt}
              onCancel={cancel}
              onToggleCamera={() => setIsCameraOpen(true)}
              onOpenDiagnostics={() => setIsDiagnosticsOpen(true)}
              onOpenManualEntry={() => setIsAttendantOpen(true)}
            />
          )}

          {(state === "awaiting_device" || state === "ready") && (
            <AwaitingDeviceScreen
              kioskName={kioskName}
              supportCode={context.supportCode}
              scannerReady={scannerReady}
              scannerFresh={scannerFresh}
              user={context.user}
              openLoans={context.openLoans}
              expiresAt={context.expiresAt}
              onReturnLoan={returnLoan}
              onClose={close}
              onToggleCamera={() => setIsCameraOpen(true)}
              onOpenDiagnostics={() => setIsDiagnosticsOpen(true)}
              onOpenManualEntry={() => setIsAttendantOpen(true)}
            />
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
      />
    </>
  );
}

export const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: KioskApp,
});

