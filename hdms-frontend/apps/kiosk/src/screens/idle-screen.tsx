import * as React from "react";
import { IdCard, QrCode, Sparkles } from "lucide-react";
import { ScreenFrame } from "./screen-frame";

export interface IdleScreenProps {
  kioskName?: string;
  supportCode?: string | null;
  scannerReady?: boolean;
  scannerFresh?: boolean;
  onToggleCamera?: () => void;
  onOpenDiagnostics?: () => void;
  onOpenManualEntry?: () => void;
}

export function IdleScreen({
  kioskName = "HDMS Kiosk",
  supportCode = "KIOSK-01",
  scannerReady = true,
  scannerFresh = true,
  onToggleCamera,
  onOpenDiagnostics,
  onOpenManualEntry,
}: IdleScreenProps) {
  React.useEffect(() => {
    if (typeof performance !== "undefined" && typeof performance.mark === "function") {
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
      onToggleCamera={onToggleCamera}
      onOpenDiagnostics={onOpenDiagnostics}
      onOpenManualEntry={onOpenManualEntry}
    >

      <div className="flex flex-col items-center text-center space-y-10 py-6 max-w-2xl">
        {/* Main Directive Prompt: ≥ 36px font-size, high-contrast, visible from a metre */}
        <div className="space-y-4">
          <div className="inline-flex items-center gap-2 rounded-full bg-primary/10 px-4 py-1.5 text-sm font-semibold text-primary">
            <Sparkles className="size-4" />
            <span>Ready for Scanner Input</span>
          </div>

          <h2
            data-testid="idle-prompt"
            className="text-4xl md:text-5xl font-extrabold tracking-tight text-foreground leading-tight"
            style={{ fontSize: "clamp(2.25rem, 5vw, 3.25rem)" }}
          >
            Scan your ID card or a device barcode
          </h2>

          <p className="text-xl text-muted-foreground max-w-lg mx-auto">
            Hold your staff ID card or device barcode directly under the optical scanner.
          </p>
        </div>

        {/* Dual Visual Guidance Cards */}
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-6 w-full pt-2">
          {/* Card 1: ID Badge */}
          <div className="flex flex-col items-center justify-center p-8 rounded-2xl border-2 border-border bg-card shadow-sm space-y-4 transition-transform hover:scale-[1.02]">
            <div className="p-4 rounded-2xl bg-primary/10 text-primary">
              <IdCard className="size-12" aria-hidden="true" />
            </div>
            <div className="space-y-1">
              <h3 className="text-xl font-bold">Staff ID Card</h3>
              <p className="text-sm text-muted-foreground">
                View your active borrowed devices or return equipment
              </p>
            </div>
          </div>

          {/* Card 2: Equipment Barcode */}
          <div className="flex flex-col items-center justify-center p-8 rounded-2xl border-2 border-border bg-card shadow-sm space-y-4 transition-transform hover:scale-[1.02]">
            <div className="p-4 rounded-2xl bg-secondary text-secondary-foreground">
              <QrCode className="size-12" aria-hidden="true" />
            </div>
            <div className="space-y-1">
              <h3 className="text-xl font-bold">Device Barcode</h3>
              <p className="text-sm text-muted-foreground">
                Quick-scan any asset tag to check out or return
              </p>
            </div>
          </div>
        </div>

        {/* Subtle Wake Hint if Scanner is Idle */}
        {!scannerFresh && scannerReady && (
          <div
            data-testid="scanner-wake-hint"
            className="rounded-xl border border-amber-500/30 bg-amber-500/10 px-6 py-3 text-sm font-medium text-amber-900 dark:text-amber-200"
          >
            Scanner in power-saving mode — press the scanner trigger once to wake it.
          </div>
        )}
      </div>
    </ScreenFrame>
  );
}
