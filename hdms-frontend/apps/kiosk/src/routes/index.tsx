import { createRoute } from "@tanstack/react-router";
import { rootRoute } from "./root";
import { useKioskSession } from "@/machine/use-kiosk-session";
import { IdleScreen } from "@/screens/idle-screen";
import { AwaitingUserScreen } from "@/screens/awaiting-user-screen";
import { AwaitingDeviceScreen } from "@/screens/awaiting-device-screen";
import { CameraOverlay } from "@/components/camera-overlay";
import { DiagnosticsModal } from "@/components/diagnostics-modal";

export function KioskApp() {
  const {
    state,
    context,
    kioskName,
    scannerReady,
    scannerFresh,
    isCameraOpen,
    isDiagnosticsOpen,
    cameraSource,
    hidSource,
    setIsCameraOpen,
    setIsDiagnosticsOpen,
    scan,
    returnLoan,
    close,
    cancel,
  } = useKioskSession();

  return (
    <>
      {/* Screen Router by Session Machine State */}
      {state === "idle" && (
        <IdleScreen
          kioskName={kioskName}
          supportCode={context.supportCode}
          scannerReady={scannerReady}
          scannerFresh={scannerFresh}
          onToggleCamera={() => setIsCameraOpen(true)}
          onOpenDiagnostics={() => setIsDiagnosticsOpen(true)}
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
        />
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
    </>
  );
}

export const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: KioskApp,
});
