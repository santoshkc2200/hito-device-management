import * as React from "react";
import { CheckCircle2, CornerDownLeft, Laptop, PackageCheck } from "lucide-react";
import type { SessionDevice, SessionMessage, OutcomeKind } from "@hdms/api-client";
import { Button } from "@/components/ui/button";
import { formatHumanDueDate } from "@/lib/date-format";
import { ScreenFrame } from "./screen-frame";

export interface SuccessScreenProps {
  kind: OutcomeKind | "borrowed" | "returned" | string;
  device?: SessionDevice | null;
  message?: SessionMessage | null;
  dueAt?: string | null;
  itemCount?: number;
  onDone: () => void;
  onScanAnother?: () => void;
  kioskName?: string;
  supportCode?: string | null;
  scannerReady?: boolean;
  scannerFresh?: boolean;
  onToggleCamera?: () => void;
  onOpenDiagnostics?: () => void;
}

export function SuccessScreen({
  kind,
  device,
  message,
  dueAt,
  itemCount = 1,
  onDone,
  onScanAnother,
  kioskName = "HDMS Kiosk",
  supportCode = "KIOSK-01",
  scannerReady = true,
  scannerFresh = true,
  onToggleCamera,
  onOpenDiagnostics,
}: SuccessScreenProps) {
  const isReturn = kind === "returned";
  const dueLine = formatHumanDueDate(dueAt);

  // Auto-dismiss after 4 seconds (4000 ms)
  React.useEffect(() => {
    const timer = setTimeout(() => {
      onDone();
    }, 4000);
    return () => clearTimeout(timer);
  }, [onDone]);

  const title =
    message?.title ||
    (isReturn ? "Device Returned Successfully" : "Device Borrowed Successfully");
  const detail = message?.detail || (isReturn ? "Return confirmed." : "Borrow recorded.");

  return (
    <ScreenFrame
      kioskName={kioskName}
      supportCode={supportCode}
      scannerReady={scannerReady}
      scannerFresh={scannerFresh}
      onToggleCamera={onToggleCamera}
      onOpenDiagnostics={onOpenDiagnostics}
    >
      <div
        data-testid="success-screen"
        className="flex flex-col items-center justify-between h-full w-full max-w-3xl space-y-6 py-4 text-center"
      >
        {/* Top Outcome Badge & Icon */}
        <div className="space-y-4 flex flex-col items-center">
          <div
            data-testid="success-icon-container"
            className={`p-4 rounded-full shadow-md ${
              isReturn
                ? "bg-blue-500/15 text-blue-600 dark:text-blue-400"
                : "bg-green-500/15 text-green-600 dark:text-green-400"
            }`}
          >
            {isReturn ? (
              <CornerDownLeft className="size-16 stroke-[2.5]" aria-hidden="true" />
            ) : (
              <CheckCircle2 className="size-16 stroke-[2.5]" aria-hidden="true" />
            )}
          </div>

          {/* Outcome Type & Multi-Item Context */}
          <div className="flex flex-wrap items-center justify-center gap-2">
            <span
              data-testid="success-kind-badge"
              className={`font-mono text-xs font-bold uppercase tracking-wider px-3 py-1 rounded-full ${
                isReturn
                  ? "bg-blue-100 text-blue-800 dark:bg-blue-950 dark:text-blue-200"
                  : "bg-green-100 text-green-800 dark:bg-green-950 dark:text-green-200"
              }`}
            >
              {isReturn ? "Return Confirmed" : "Borrow Confirmed"}
            </span>

            {itemCount > 1 && (
              <span
                data-testid="multi-item-badge"
                className="inline-flex items-center gap-1.5 font-sans text-xs font-bold px-3 py-1 rounded-full bg-secondary text-secondary-foreground shadow-xs"
              >
                <PackageCheck className="size-3.5 text-primary" />
                <span>{itemCount} items</span>
              </span>
            )}
          </div>

          {/* Primary Outcome Title */}
          <h2
            data-testid="success-title"
            className="text-4xl md:text-5xl font-extrabold tracking-tight text-foreground"
            style={{ fontSize: "clamp(2.25rem, 5vw, 3rem)" }}
          >
            {title}
          </h2>

          <p data-testid="success-detail" className="text-lg md:text-xl text-muted-foreground max-w-xl">
            {detail}
          </p>
        </div>

        {/* Device Information Card */}
        {device && (
          <div
            data-testid="success-device-card"
            className="w-full max-w-lg rounded-2xl border-2 border-border bg-card p-6 shadow-sm flex flex-col items-center space-y-3"
          >
            <div className="flex items-center gap-3">
              <div className="p-2.5 rounded-lg bg-secondary text-secondary-foreground">
                <Laptop className="size-6 text-primary" />
              </div>
              <div className="text-left">
                <p className="text-xl font-bold text-foreground">{device.name}</p>
                <p className="font-mono text-xs font-semibold text-muted-foreground">
                  Asset Tag: <span className="text-foreground">{device.assetTag}</span>
                </p>
              </div>
            </div>

            {/* Due date line (strictly omitted when null) */}
            {dueLine && (
              <div
                data-testid="success-due-line"
                className="w-full pt-3 border-t border-border/80 text-center"
              >
                <p className="text-sm font-semibold text-foreground/90 bg-muted/60 py-1.5 px-3 rounded-lg">
                  {dueLine}
                </p>
              </div>
            )}
          </div>
        )}

        {/* Bottom Actions and Auto-Return Notice */}
        <div className="w-full max-w-lg space-y-4 pt-2">
          <p className="text-sm text-muted-foreground">
            Scan another device, or tap Done (auto-closing in 4s)
          </p>

          <div className="flex flex-col sm:flex-row gap-3">
            {onScanAnother && (
              <Button
                type="button"
                variant="outline"
                size="lg"
                data-testid="scan-another-button"
                className="min-h-16 flex-1 text-lg font-bold"
                onClick={onScanAnother}
              >
                Scan Another
              </Button>
            )}

            <Button
              type="button"
              variant="default"
              size="lg"
              data-testid="done-success-button"
              className="min-h-16 flex-1 text-xl font-bold bg-primary text-primary-foreground shadow-lg hover:bg-primary/90"
              onClick={onDone}
            >
              Done
            </Button>
          </div>
        </div>
      </div>
    </ScreenFrame>
  );
}
