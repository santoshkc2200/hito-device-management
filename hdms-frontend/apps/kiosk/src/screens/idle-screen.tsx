import * as React from "react";
import { IdCard, QrCode, ScanLine, Sparkles } from "lucide-react";
import { Button } from "@/components/ui/button";
import { ScreenFrame } from "./screen-frame";

export interface IdleScreenProps {
  kioskName?: string;
  supportCode?: string | null;
  scannerReady?: boolean;
  scannerFresh?: boolean;
  isScanning?: boolean;
  onStartScanning?: () => void;
  onStartCamera?: () => void;
  onOpenDiagnostics?: () => void;
  onOpenManualEntry?: () => void;
}

export function IdleScreen({
  kioskName = "HDMS Kiosk",
  supportCode = "KIOSK-01",
  scannerReady = true,
  scannerFresh = true,
  isScanning = false,
  onStartScanning,
  onStartCamera,
  onOpenDiagnostics,
  onOpenManualEntry,
}: IdleScreenProps) {
  React.useEffect(() => {
    if (
      typeof performance !== "undefined" &&
      typeof performance.mark === "function"
    ) {
      try {
        performance.mark("kiosk-idle-rendered");
      } catch {
        // Ignore duplicate mark errors
      }
    }
  }, []);

  return (
    <ScreenFrame
      kioskName={kioskName}
      supportCode={supportCode}
      scannerReady={scannerReady}
      scannerFresh={scannerFresh}
      onToggleCamera={isScanning ? onStartCamera : undefined}
      onOpenDiagnostics={onOpenDiagnostics}
      onOpenManualEntry={onOpenManualEntry}
    >
      <div className="flex w-full max-w-4xl flex-col items-center gap-8 py-6 text-center">
        {/* Persistent scanner status remains separate from scan guidance. */}
        <div className="space-y-4">
          <div className="inline-flex items-center gap-2 rounded-full bg-primary/10 px-4 py-1.5 text-sm font-semibold text-primary">
            <Sparkles className="size-4" />
            <span>
              {isScanning
                ? scannerReady
                  ? "Ready to Scan"
                  : "Scanner Not Detected"
                : "Ready for Scanner Input"}
            </span>
          </div>
          {isScanning && (
            <p
              data-testid="scan-status"
              role="status"
              aria-live="polite"
              className="mx-auto max-w-lg text-kiosk-body text-muted-foreground"
            >
              {scannerReady
                ? "Scan with the device scanner, or use the camera instead."
                : "Device scanner not detected."}
            </p>
          )}
        </div>

        <div
          data-testid="idle-action-stack"
          className="flex w-full max-w-4xl flex-col items-stretch justify-center gap-6 min-[700px]:flex-row"
        >
          {!isScanning && onStartScanning && (
            <Button
              type="button"
              data-testid="start-scanning-button"
              variant="outline"
              size="lg"
              className="aspect-[1.3] min-h-48 w-full max-w-sm flex-col gap-4 self-center rounded-2xl border-2 border-primary/25 bg-card p-5 text-2xl font-bold text-card-foreground shadow-kiosk-card hover:bg-accent hover:text-accent-foreground min-[700px]:w-64 min-[700px]:shrink-0"
              onClick={onStartScanning}
            >
              <span className="flex size-16 items-center justify-center rounded-2xl bg-primary/10 text-primary">
                <ScanLine className="size-10" aria-hidden="true" />
              </span>
              <span>Start</span>
            </Button>
          )}

          {/* One informational card shows the two equivalent scan choices. */}
          <div
            data-testid="scan-guidance-card"
            className="w-full min-w-0 max-w-sm self-center overflow-hidden rounded-2xl border-2 border-border bg-card text-left shadow-kiosk-card min-[700px]:max-w-none min-[700px]:flex-1"
          >
            <div className="border-b border-border p-5">
              <h2
                data-testid="idle-prompt"
                className="text-kiosk-body leading-snug font-semibold text-sm text-muted-foreground text-card-foreground"
              >
                Tap Start, then scan your staff ID card or a device barcode in
                any order.
              </h2>
            </div>

            <div
              data-testid="staff-id-card"
              className="flex min-h-20 items-center gap-4 p-4"
            >
              <div className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary">
                <IdCard className="size-7" aria-hidden="true" />
              </div>
              <div className="min-w-0 space-y-0.5">
                <p className="text-primary font-semibold">Staff ID Card</p>
              </div>
            </div>

            <div
              data-testid="device-barcode-card"
              className="flex min-h-20 items-center gap-4 border-t border-border p-4"
            >
              <div className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary">
                <QrCode className="size-7" aria-hidden="true" />
              </div>
              <div className="min-w-0 space-y-0.5">
                <p className="text-primary font-semibold">Device Barcode</p>
              </div>
            </div>
          </div>
        </div>

        {/* Subtle Wake Hint if Scanner is Idle */}
        {!isScanning && !scannerFresh && scannerReady && (
          <div
            data-testid="scanner-wake-hint"
            className="rounded-xl border border-amber-500/30 bg-amber-500/10 px-6 py-3 text-sm font-medium text-amber-900 dark:text-amber-200"
          >
            Scanner in power-saving mode — press the scanner trigger once to
            wake it.
          </div>
        )}
      </div>
    </ScreenFrame>
  );
}
